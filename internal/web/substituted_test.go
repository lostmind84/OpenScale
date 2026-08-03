package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"openscale/internal/domain"
)

// --- Un fichier dont un bloc n'a pas décodé, porte par porte -----------------
//
// The callers of ConfigStore.Read do three different things with the file, and each
// needs its own answer. One flat verdict for all of them is what produced a defect in each
// direction on 02/08/2026 (domain.UnreadableBlocksError). One test per door, below.

// TestTheAdminScreenShowsTheReadBlocksAndNamesTheSubstitutedOnes is the DISPLAY door.
//
// The screen must show what the file really says — a station out of service runs the
// factory profile, and feeding the screen from memory is « la différence entre le réparer
// et le détruire » — and it must say which blocks it could NOT read, or a volunteer saves
// the factory tariffs over the shop's own without ever being told.
func TestTheAdminScreenShowsTheReadBlocksAndNamesTheSubstitutedOnes(t *testing.T) {
	b, _, shop := benchOverADamagedFile(t, nil)

	got := decodeStatus[configDTO](t, b.get("/admin/api/config"), http.StatusOK)

	var served domain.Config
	if err := json.Unmarshal(got.Config, &served); err != nil {
		// The payload is re-marshalled from a decoded Config, so the damaged block travels
		// as the neutral one and this always parses.
		t.Fatalf("charge illisible : %v", err)
	}
	if served.Station.Coop != shop.Station.Coop {
		t.Errorf("station.coop = %q, attendu %q : l'écran montre la mémoire, pas le fichier",
			served.Station.Coop, shop.Station.Coop)
	}
	if len(got.Unreadable) != 1 {
		t.Fatalf("%d bloc(s) signalé(s) comme illisible(s), attendu 1 : %+v", len(got.Unreadable),
			got.Unreadable)
	}
	if got.Unreadable[0].Field != "pricing" {
		t.Errorf("le bloc signalé est %q, attendu pricing", got.Unreadable[0].Field)
	}
	if got.Unreadable[0].Message == "" {
		t.Error("le bloc est nommé sans dire pourquoi il n'a pas été lu")
	}
}

// TestRestoringABackupWithAnUnreadableBlockIsNotAMissingVersion is the RESTORE door.
//
// The backup is right there, listed on the screen one line above the button. « Introuvable »
// sends a volunteer looking for a file that exists; what is true is that it cannot be
// applied as it stands, which is what the validation branch beside it already answers.
func TestRestoringABackupWithAnUnreadableBlockIsNotAMissingVersion(t *testing.T) {
	b, path, _ := benchOverADamagedFile(t, nil)
	// .1 is a copy of the damaged file: a backup taken before somebody hand-edited it badly.
	if err := os.WriteFile(path+".1", readRaw(t, path), 0o644); err != nil {
		t.Fatalf("écriture de la sauvegarde : %v", err)
	}
	b.setPassword("mot-de-passe-long", "ABCD2345")
	b.login("mot-de-passe-long")

	response := b.post("/admin/api/config/restore", `{"version":1}`)

	if response.StatusCode == http.StatusNotFound {
		t.Fatal("une sauvegarde qui existe a été annoncée introuvable")
	}
	got := decodeStatus[problem](t, response, http.StatusUnprocessableEntity)
	if !strings.Contains(got.Message, "pricing") {
		t.Errorf("le refus ne nomme pas le bloc : %q", got.Message)
	}
	if len(got.Faults) == 0 {
		t.Error("le refus ne porte pas la raison, que l'écran affiche champ par champ")
	}
}

// TestReloadingAFileWithAnUnreadableBlockIsRefusedByName is the PUT-IN-SERVICE door, and
// the one caller for which refusing is the whole right answer: the station would run the
// factory tariffs while its file declares the shop's.
func TestReloadingAFileWithAnUnreadableBlockIsRefusedByName(t *testing.T) {
	b, _, _ := benchOverADamagedFile(t, nil)
	b.setPassword("mot-de-passe-long", "ABCD2345")
	b.login("mot-de-passe-long")

	response := b.post("/admin/api/config/reload", "")

	got := decodeStatus[problem](t, response, http.StatusUnprocessableEntity)
	if !strings.Contains(got.Message, "pricing") {
		t.Errorf("le refus ne nomme pas le bloc à ouvrir : %q", got.Message)
	}
	if b.hub.Config().Station.Coop != domain.NeutralProfile().Station.Coop {
		t.Error("le poste s'est mis à tourner sur un fichier dont un bloc est celui d'usine")
	}
}

// TestSavingOverAFileWithAnUnreadableBlockKeepsTheCatalogPassword is the REWRITE door, and
// the trap is second-order: `served` is what the submitted document is compared against,
// and a read treated as a failure made it the configuration IN FORCE — the neutral profile,
// whose catalog carries no password. A save about anything at all then erased a producer's
// WebDAV account, silently.
func TestSavingOverAFileWithAnUnreadableBlockKeepsTheCatalogPassword(t *testing.T) {
	const account = "s3cr3t-du-producteur"
	b, path, shop := benchOverADamagedFile(t, func(cfg *domain.Config) {
		cfg.Catalog.Options["password"] = json.RawMessage(`"` + account + `"`)
	})
	b.setPassword("mot-de-passe-long", "ABCD2345")
	b.login("mot-de-passe-long")

	// What the screen received, edited on one harmless field, and sent back. The password
	// is never served, so it is never resubmitted: carriedOverSecret has to put it back.
	served := decodeStatus[configDTO](t, b.get("/admin/api/config"), http.StatusOK)
	var next domain.Config
	if err := json.Unmarshal(served.Config, &next); err != nil {
		t.Fatalf("charge illisible : %v", err)
	}
	next.Journal.MaxRows = shop.Journal.MaxRows + 100

	response := b.do(http.MethodPut, "/admin/api/config", marshal(t, next), nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("PUT = %d : %s", response.StatusCode, body(t, response))
	}
	response.Body.Close()

	if !bytes.Contains(readRaw(t, path), []byte(account)) {
		t.Error("le compte WebDAV du producteur a été effacé par un enregistrement qui ne " +
			"le concernait pas")
	}
}

// TestAnUnconfirmedRestorationOverAnUnreadableBlockPutsTheShopsFileBack is the ROLLBACK
// door, and the one whose failure nobody is standing in front of.
//
// The restoration arms the sixty-second countdown of §11.4, and what the countdown writes
// back is FileBefore. Leaving it nil — which is what a read treated as a plain failure
// does — makes the rollback fall back on the configuration IN SERVICE, and on a station
// that started out of service that is the neutral profile. The shop's file is therefore
// overwritten with the factory one a full minute after the volunteer walked away, with
// nothing on any screen.
//
// Everything is real: a file on disk, a platform.ConfigStore, the station's own rollback.
func TestAnUnconfirmedRestorationOverAnUnreadableBlockPutsTheShopsFileBack(t *testing.T) {
	b, path, shop := benchOverADamagedFile(t, nil)
	// A backup that differs on the HARDWARE, so the restoration arms a countdown at all.
	backup := reread(t, shop)
	backup.Scale.Options["port"] = json.RawMessage(`"COM9"`)
	writeRawConfig(t, path+".1", backup)
	b.setPassword("mot-de-passe-long", "ABCD2345")
	b.login("mot-de-passe-long")

	restored := decodeStatus[configDTO](t,
		b.post("/admin/api/config/restore", `{"version":1}`), http.StatusOK)
	if restored.Pending == nil {
		t.Fatal("restaurer une version qui change le matériel n'arme aucun compte à rebours")
	}

	// Nobody confirms.
	b.advance(61 * time.Second)
	written := awaitFileWithout(t, b, path, "COM9")

	if written.Station.Coop != shop.Station.Coop {
		t.Errorf("station.coop = %q, attendu %q : le retour arrière a écrit le profil "+
			"d'usine sur le fichier du magasin, soixante secondes après",
			written.Station.Coop, shop.Station.Coop)
	}
	if written.Catalog.Type != shop.Catalog.Type {
		t.Errorf("catalog.type = %q, attendu %q : la source du catalogue a été remplacée "+
			"par le retour arrière", written.Catalog.Type, shop.Catalog.Type)
	}
	if written.Limits.BasketMin != shop.Limits.BasketMin {
		t.Errorf("limits.basket_min = %v, attendu %v : les garde-fous ont été remplacés",
			written.Limits.BasketMin, shop.Limits.BasketMin)
	}
}

// awaitFileWithout waits until the file on disk no longer carries the unconfirmed port,
// which is what says the rollback has run, and returns it decoded the way a station decodes
// it.
func awaitFileWithout(t *testing.T, b *bench, path, unconfirmedPort string) domain.Config {
	t.Helper()
	deadline := time.Now().Add(hang)
	for time.Now().Before(deadline) {
		// A transient read failure is EXPECTED here and is not the answer: §11.4 replaces
		// the file by renaming a temporary over it, and on Windows that window is an open
		// that fails. Polling through it is what makes this test about the rollback rather
		// than about the atomic write beside it.
		if raw, err := os.ReadFile(path); err == nil {
			written, _ := domain.DecodeConfigBlockByBlock(raw)
			if port, declared := written.Scale.Options.Text("port"); !declared || port != unconfirmedPort {
				return written
			}
		}
		b.clock.Advance(time.Second)
		time.Sleep(time.Millisecond)
	}
	t.Fatal("le fichier porte encore la configuration non confirmée : le retour arrière ne " +
		"l'a jamais réécrit, et le prochain démarrage repartirait dessus")
	return domain.Config{}
}
