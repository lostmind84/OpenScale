package main

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openscale/internal/domain"
)

// §11.3: an unusable configuration NEVER kills the process. The station comes up on the
// neutral profile, in the one terminal state, and serves the whole list of faults — with
// the two things that must survive the fallback, because both were found by trying to
// repair a station from its own screen: the KEYS (who may repair it) and the DOOR (the
// address it is reached on).

// TestAnUnreadableConfigurationRefusesToServe is the other half of §11.3.
//
// A configuration that is INVALID never kills the process: the station starts on the
// neutral profile and serves the whole list of faults, which is the assertion of
// TestAnInvalidConfigurationStillServes below, and — since porte 1 — of
// TestATrulyBrokenConfigurationStillServes too: even a document that is not JSON at all
// falls back rather than refusing. A file that cannot be READ AT ALL is a different fact —
// there is no station number, no listening address and nothing an administration screen
// could safely write back — and it alone refuses, in French, naming the file, with a
// non-zero exit code and NO PANIC.
func TestAnUnreadableConfigurationRefusesToServe(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "config.json")

	// A BOUNDED context, so that a subcommand which starts anyway fails here instead of
	// hanging: a station built on a configuration nobody could read would listen on an
	// address nobody chose and wait for a signal for ever, and a test that hangs says
	// nothing to whoever broke it.
	ctx, cancel := context.WithTimeout(context.Background(), startBudget)
	defer cancel()

	var out bytes.Buffer
	err := runServe(ctx, []string{"--config", missing, "--data", t.TempDir()}, &out)
	if err == nil {
		t.Fatal("un fichier absent a laissé le poste démarrer")
	}
	if code := exitCodeFor(err); code == 0 {
		t.Fatalf("code de sortie %d : un démarrage refusé doit être visible du gestionnaire de service", code)
	}
	message := explain(err)
	if !strings.Contains(message, missing) {
		t.Fatalf("le refus ne nomme pas le fichier fautif : %s", message)
	}
	if !strings.Contains(message, "configuration") {
		t.Fatalf("le refus n'est pas en français et ne dit pas de quoi il parle : %s", message)
	}
}

// TestATrulyBrokenConfigurationStillServes is porte 1 (LoadConfig,
// TestLoadConfigOfATruncatedFileIsNotAnError), exercised at the level `serve` runs at.
//
// A document that is not JSON at all used to refuse to start, alongside a missing file.
// It no longer does: DecodeConfigBlockByBlock falls back to the neutral profile for a
// document it cannot decode at all exactly as it does for one bad block, and the station
// serves ERR-CFG-01 like any other invalid configuration
// (TestAnInvalidConfigurationStillServes) — the file is illisible, but it EXISTS, and a
// wrong path in a service unit is the one case that must still refuse.
func TestATrulyBrokenConfigurationStillServes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("{\"station\": {\"number\": "), 0o644); err != nil {
		t.Fatalf("écriture du fichier cassé : %v", err)
	}

	b := &serveBench{
		t:          t,
		configPath: path,
		dataDir:    filepath.Join(dir, "data"),
		out:        &syncBuffer{},
		returned:   make(chan error, 1),
		client:     &http.Client{},
	}
	// --listen, and not the neutral profile's own 127.0.0.1:8085: that address is shared
	// by every station of the parc, including the one this developer may have installed
	// on their own machine.
	b.options = serveOptions{configPath: path, dataDir: b.dataDir, listen: freeAddress(t)}
	b.start()

	live := b.get("/healthz")
	if live.StatusCode != http.StatusOK {
		t.Fatalf("/healthz = %d : un document illisible a tué le poste", live.StatusCode)
	}
	_ = live.Body.Close()

	if got := b.output(); !strings.Contains(got, "ERR-CFG-01") {
		t.Fatalf("la sortie ne nomme pas ERR-CFG-01 :\n%s", got)
	}

	if err := b.stop(); err != nil {
		t.Fatalf("serve a rendu une erreur sur un arrêt demandé : %v", err)
	}
}

// TestAnInvalidConfigurationStillServes is the guiding principle 7 of §11.3: « le poste
// démarre toujours ».
//
// A negative discount would print a negative price, so control 13 refuses it. The
// station starts anyway, on the neutral profile loaded IN MEMORY AND NEVER WRITTEN,
// in the one terminal state, and it serves — because a broken configuration must
// never produce a black screen, and because the screen that fixes it is served by
// the very process the configuration broke.
func TestAnInvalidConfigurationStillServes(t *testing.T) {
	bench := newServeBench(t, func(cfg *domain.Config) {
		cfg.Pricing.Tiers[0].Discount = -10
	})
	bench.start()

	live := bench.get("/healthz")
	if live.StatusCode != http.StatusOK {
		t.Fatalf("/healthz = %d : une configuration invalide a tué le poste", live.StatusCode)
	}
	_ = live.Body.Close()

	if got := bench.output(); !strings.Contains(got, "ERR-CFG-01") {
		t.Fatalf("la sortie ne nomme pas ERR-CFG-01 :\n%s", got)
	}
	if got := bench.output(); !strings.Contains(got, "discount_percent") {
		t.Fatalf("la liste des fautes ne nomme pas le champ fautif :\n%s", got)
	}
	// The file on disk is UNTOUCHED: the neutral profile is loaded in memory and
	// nothing writes it back over what an operator typed.
	raw, err := os.ReadFile(bench.configPath)
	if err != nil {
		t.Fatalf("relecture de la configuration : %v", err)
	}
	if !bytes.Contains(raw, []byte(`"discount_percent": -1`)) {
		t.Fatalf("le fichier fautif a été réécrit par le poste :\n%s", raw)
	}

	if err := bench.stop(); err != nil {
		t.Fatalf("serve a rendu une erreur sur un arrêt demandé : %v", err)
	}
}

// TestTheFallbackProfileKeepsTheKEYSToTheStation.
//
// §11.3 replaces the configuration a station OPERATES ON when that configuration is
// unusable. It has no business replacing the identity of whoever administers it — and
// dropping it locked the screen on the one station §11.3 exists to keep serving: the
// login form answered « aucun mot de passe n'est défini » and the recovery form « ce
// poste n'a pas de code de secours », about a file that carried both.
func TestTheFallbackProfileKeepsTheKEYSToTheStation(t *testing.T) {
	broken := shippedConfig(t)
	broken.Admin.PasswordHash = "$argon2id$v=19$m=65536,t=3,p=2$c2VsLWRlLXRlc3Q$Y2xlLWRlLXRlc3QtcG91ci1jZS1wb3N0ZQ"
	broken.Admin.RecoveryCodeHash = "$argon2id$v=19$m=65536,t=3,p=2$YXV0cmUtc2VsLTAx$Y2xlLWR1LWNvZGUtZGUtc2Vjb3Vycy1pY2k"
	broken.Station.Coop = "Les Amis de la Coopé"

	fallback := fallbackProfile(broken, faultsOn("pricing.tiers[0].discount_percent"))
	if fallback.Admin.PasswordHash != broken.Admin.PasswordHash {
		t.Error("le profil de repli oublie le mot de passe d'administration du fichier")
	}
	if fallback.Admin.RecoveryCodeHash != broken.Admin.RecoveryCodeHash {
		t.Error("le profil de repli oublie le code de secours du fichier")
	}
	// Everything the station OPERATES on is the neutral profile, and nothing else is
	// borrowed from a file that carries faults.
	if fallback.Station.Coop == broken.Station.Coop {
		t.Error("le profil de repli fait tourner le poste sur la configuration fautive")
	}
}

// TestTheFallbackProfileKeepsTheDOORToTheStation is the same rule applied to the network
// block, and it is the rule the bench of 2026-07-29 discovered the hard way.
//
// The keys are useless behind a door nobody can find. §11.3 replaces what a station RUNS
// ON, never the way one REACHES it in order to repair it: the address a station answers
// on is written on the installation sheet and dialled by the kiosk from that same file,
// and admin_on_lan is what lets a volunteer arrive with a laptop rather than a keyboard.
// Borrowing the neutral 127.0.0.1:8085 moved the service off the address the file
// declares while the kiosk kept opening it — a black client screen — and shut the
// administration screen back onto the loopback at the worst possible moment.
func TestTheFallbackProfileKeepsTheDOORToTheStation(t *testing.T) {
	broken := shippedConfig(t)
	broken.Network = domain.NetworkConfig{Listen: "127.0.0.1:8099", AdminOnLAN: true}

	fallback := fallbackProfile(broken, faultsOn("pricing.tiers[0].discount_percent"))
	if fallback.Network.Listen != broken.Network.Listen {
		t.Errorf("adresse du repli = %q, attendu %q : le repli jette une adresse d'écoute qui "+
			"n'est pas fautive", fallback.Network.Listen, broken.Network.Listen)
	}
	if !fallback.Network.AdminOnLAN {
		t.Error("le repli referme l'écran d'administration sur la boucle locale, au moment " +
			"même où un bénévole vient réparer le poste depuis son portable")
	}
}

// TestTheFallbackProfileTakesTheNeutralAddressWhenTheFileAddressIsItselfFaulted is what
// keeps the test above from being an invitation to copy an unbindable address.
//
// When the faults name the network block, the file has nothing usable to lend: the
// neutral profile provides the address, exactly as before. Without this case, a fallback
// that copied « 127.0.0.1 » — no port — would turn ERR-CFG-01, a station serving its
// fault list, into ERR-SYS-02, a station that is not there at all.
func TestTheFallbackProfileTakesTheNeutralAddressWhenTheFileAddressIsItselfFaulted(t *testing.T) {
	broken := shippedConfig(t)
	broken.Network = domain.NetworkConfig{Listen: "127.0.0.1", AdminOnLAN: true}

	fallback := fallbackProfile(broken, faultsOn("network.listen"))
	if want := domain.NeutralProfile().Network.Listen; fallback.Network.Listen != want {
		t.Errorf("adresse du repli = %q, attendu %q : le repli a recopié une adresse "+
			"inliable", fallback.Network.Listen, want)
	}
	if fallback.Network.AdminOnLAN {
		t.Error("le repli a gardé la moitié d'un bloc network fautif : l'écran " +
			"d'administration s'ouvre au réseau sur une configuration que personne n'a validée")
	}
}

// faultsOn builds the verdict of a Validate that found exactly these fields wrong.
func faultsOn(fields ...string) []domain.Fault {
	faults := make([]domain.Fault, 0, len(fields))
	for _, field := range fields {
		faults = append(faults, domain.Fault{Field: field, Message: "faute de banc d'essai"})
	}
	return faults
}

// TestAFaultyConfigurationStillServesOnTheAddressItsFileDeclares is what the bench of
// 2026-07-29 paid for, and the assertion no test made until it did.
//
// The station shipped that day carried network.listen 8099 AND eight faults elsewhere.
// The fallback threw the address out with the rest, the service came up on the
// 127.0.0.1:8085 of the neutral profile, and the kiosk — which reads the FILE, and reads
// it successfully because a faulty file is still a readable one — opened 8099. A black
// client screen, on the very station §11.3 exists to keep alive.
//
// So: a fault ANYWHERE BUT on the address, an address in the file, no flag, and the
// station must serve on the address its file names.
func TestAFaultyConfigurationStillServesOnTheAddressItsFileDeclares(t *testing.T) {
	bench := newServeBench(t, func(cfg *domain.Config) {
		cfg.Pricing.Tiers[0].Discount = -10
	}).listenFlag("")
	bench.start()

	if bench.address != bench.fileAddress {
		t.Fatalf("le poste sert sur %q alors que son fichier déclare %q : le repli a jeté une "+
			"adresse d'écoute qui n'était pas fautive, et l'écran client ouvre une adresse que "+
			"rien ne sert", bench.address, bench.fileAddress)
	}
	// And it really is the fallback that is being observed, not a configuration that
	// turned out to be valid after all.
	if got := bench.output(); !strings.Contains(got, "ERR-CFG-01") {
		t.Fatalf("la sortie ne nomme pas ERR-CFG-01 : ce banc ne traverse pas le repli\n%s", got)
	}

	if err := bench.stop(); err != nil {
		t.Fatalf("serve a rendu une erreur sur un arrêt demandé : %v", err)
	}
}
