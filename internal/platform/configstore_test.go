package platform

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"openscale/internal/domain"
)

// The write of §11.4 is what stands between an administration screen and a station that
// cannot start any more. These tests drive it the only way that proves anything: by
// interrupting it.

// TestAnInterruptedWriteLeavesTheConfigurationInForce is the assertion of §11.4, step 4.
//
// The rename is the ONE instant the content changes. Everything before it — the temporary
// file, its fsync, the rotation — must leave config.json exactly as it was, because that
// file is what the station reads at the next start and what a volunteer opens when
// nothing else works.
//
// The interruption is produced through the rename seam and not by killing a process: a
// power cut is not something `go test` arranges, and a test that could not produce the
// failure would be a comment claiming the property.
func TestAnInterruptedWriteLeavesTheConfigurationInForce(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "config.json")
	before := writeStation(t, path, 2)

	store := newStore(t, path)
	cut := errors.New("coupure de courant entre le fichier temporaire et le rename")
	// The seam is a FIELD of the store and not an exported hook: this test lives in the
	// package, and the production path has no way to reach it.
	store.rename = func(from, to string) error {
		if to == path {
			return cut
		}
		return os.Rename(from, to)
	}

	next := before
	next.Station.Number = 3
	err := store.Save(context.Background(), next)
	if err == nil {
		t.Fatal("une écriture interrompue s'est déclarée réussie : rien ne protège plus la " +
			"configuration en service")
	}
	if !errors.Is(err, cut) {
		t.Fatalf("le refus n'enveloppe pas la cause : %v", err)
	}
	if !strings.Contains(err.Error(), path) {
		t.Fatalf("le refus ne nomme pas le fichier qui reste en service : %v", err)
	}

	after := readConfig(t, path)
	if after.Station.Number != 2 {
		t.Fatalf("station.number = %d après une écriture interrompue, attendu 2 : "+
			"la configuration en service a été touchée", after.Station.Number)
	}
	// And nothing was left lying about: a directory filling with config-*.json is how a
	// volunteer ends up unable to tell which file the station reads.
	if leftovers := match(t, directory, "config-*.json"); len(leftovers) != 0 {
		t.Fatalf("fichiers temporaires laissés derrière : %v", leftovers)
	}
}

// TestSaveRefusesAConfigurationCarryingARetiredKey is the choke point of ADR-034.
//
// Marshalling a Config back to JSON DROPS what UnmarshalJSON could not claim, so
// writing coef_num here is how a station serialises the discount it stood for away
// -- decoding clean on the very next read, with control 20 finding nothing left to
// refuse. The configuration is seeded with os.WriteFile and not through Save: seeding
// through the very call under test would prove nothing.
func TestSaveRefusesAConfigurationCarryingARetiredKey(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "config.json")
	store := newStore(t, path)

	var legacy domain.Config
	raw := []byte(`{"pricing":{"tiers":[{"code":"MEMBER","coef_num":9}]}}`)
	if err := json.Unmarshal(raw, &legacy); err != nil {
		t.Fatalf("décodage : %v", err)
	}

	if err := store.Save(context.Background(), legacy); err == nil {
		t.Fatal("une configuration portant coef_num a été écrite : la remise que " +
			"cette clé portait vient de disparaître du fichier")
	} else if !strings.Contains(err.Error(), "coef_num") {
		t.Fatalf("le refus ne nomme pas coef_num : %v", err)
	}
	if fileExists(path) {
		t.Fatal("un fichier a été créé alors que le contrôle 20 refuse la configuration")
	}
}

// TestSaveWritesAConfigurationCarryingNoRetiredKey is the happy path through the SAME
// guard: nothing legitimate is blocked, because Retired is only ever filled by
// UnmarshalJSON and a configuration built in Go — the neutral profile, here — carries
// none.
func TestSaveWritesAConfigurationCarryingNoRetiredKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	store := newStore(t, path)

	if err := store.Save(context.Background(), domain.NeutralProfile()); err != nil {
		t.Fatalf("une configuration légitime a été refusée : %v", err)
	}
	if !fileExists(path) {
		t.Fatal("l'enregistrement n'a rien écrit")
	}
}

// TestTheVersionsRotateAndTheSixthIsDropped is §11.1: config.json.1 … .5, and no sixth.
//
// The rotation is checked by CONTENT and not by counting files: what an operator restores
// is « la configuration d'avant-hier », so the assertion has to be that version 3 really
// carries the station number that was in force three saves ago.
func TestTheVersionsRotateAndTheSixthIsDropped(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "config.json")
	cfg := writeStation(t, path, 1)
	store := newStore(t, path)

	// Seven saves, so that the rotation has had to drop two.
	for number := 2; number <= 8; number++ {
		cfg.Station.Number = number
		if err := store.Save(context.Background(), cfg); err != nil {
			t.Fatalf("enregistrement n° %d : %v", number, err)
		}
	}

	if got := readConfig(t, path).Station.Number; got != 8 {
		t.Fatalf("station.number en service = %d, attendu 8", got)
	}
	for version, want := range map[int]int{1: 7, 2: 6, 3: 5, 4: 4, 5: 3} {
		restored, err := store.Restore(context.Background(), version)
		if err != nil {
			t.Fatalf("restauration de la version %d : %v", version, err)
		}
		if restored.Station.Number != want {
			t.Fatalf("version %d : station.number = %d, attendu %d",
				version, restored.Station.Number, want)
		}
	}
	if sixth := path + ".6"; fileExists(sixth) {
		t.Fatalf("%s existe : cinq versions sont conservées, pas six", sixth)
	}
}

// TestRestoringNeverAppliesAnything is the half of §11.4 that is easy to get wrong.
//
// Restore READS a version back. Applying it goes through the same validation and the same
// atomic write as any other save, because a version that was legitimate last month may
// name a print queue this station no longer has.
func TestRestoringNeverAppliesAnything(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := writeStation(t, path, 4)
	store := newStore(t, path)

	cfg.Station.Number = 5
	if err := store.Save(context.Background(), cfg); err != nil {
		t.Fatalf("enregistrement : %v", err)
	}
	if _, err := store.Restore(context.Background(), 1); err != nil {
		t.Fatalf("restauration : %v", err)
	}
	if got := readConfig(t, path).Station.Number; got != 5 {
		t.Fatalf("station.number = %d après une restauration : elle a appliqué quelque chose", got)
	}
}

// TestAVersionOutsideTheFiveIsRefusedByName keeps the message actionable: a screen asking
// for version 9 gets the range, not the word « invalide ».
func TestAVersionOutsideTheFiveIsRefusedByName(t *testing.T) {
	store := newStore(t, filepath.Join(t.TempDir(), "config.json"))
	for _, version := range []int{0, -1, 6, 99} {
		_, err := store.Restore(context.Background(), version)
		if err == nil {
			t.Fatalf("version %d acceptée", version)
		}
		if !strings.Contains(err.Error(), "1") || !strings.Contains(err.Error(), "5") {
			t.Fatalf("le refus de la version %d ne dit pas quelles versions existent : %v",
				version, err)
		}
	}
}

// TestVersionsCarryTheirFingerprint is what makes the page Poste of §14.4 usable: the
// eight characters are how a volunteer tells two versions apart, and the date is what
// they read to choose.
func TestVersionsCarryTheirFingerprint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := writeStation(t, path, 1)
	store := newStore(t, path)

	stamped := cfg
	stamped.ModifiedAt = time.Date(2026, 7, 24, 9, 30, 0, 0, time.UTC)
	stamped.Station.Number = 2
	if err := store.Save(context.Background(), stamped); err != nil {
		t.Fatalf("enregistrement : %v", err)
	}
	// A second save, so that .1 carries the stamped one.
	stamped.Station.Number = 3
	if err := store.Save(context.Background(), stamped); err != nil {
		t.Fatalf("enregistrement : %v", err)
	}

	versions, err := store.Versions(context.Background())
	if err != nil {
		t.Fatalf("Versions : %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("%d version(s), attendu 2 après deux enregistrements", len(versions))
	}
	if versions[0].Version != 1 {
		t.Fatalf("la première version listée est %d, attendu 1 (la plus récente)", versions[0].Version)
	}
	first := versions[0]
	if len(first.Fingerprint) != 8 {
		t.Fatalf("empreinte %q : l'écran en affiche huit caractères", first.Fingerprint)
	}
	if !first.ModifiedAt.Equal(stamped.ModifiedAt) {
		t.Fatalf("modified_at = %s, attendu celui que porte le fichier (%s)",
			first.ModifiedAt, stamped.ModifiedAt)
	}
}

// TestAnUnparseableBackupIsStillListed keeps the count honest.
//
// Hiding a corrupt backup would make the screen say « 1 version » about a directory
// holding two files, and the operator would restore the wrong one.
func TestAnUnparseableBackupIsStillListed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeStation(t, path, 1)
	if err := os.WriteFile(path+".1", []byte("{ceci n'est pas du JSON"), 0o644); err != nil {
		t.Fatalf("écriture du fichier abîmé : %v", err)
	}

	versions, err := newStore(t, path).Versions(context.Background())
	if err != nil {
		t.Fatalf("Versions : %v", err)
	}
	if len(versions) != 1 {
		t.Fatalf("%d version(s), attendu 1 : une sauvegarde illisible reste listée", len(versions))
	}
	if versions[0].Fingerprint != "" {
		t.Fatalf("empreinte %q sur une sauvegarde illisible : elle est inconnue, pas inventée",
			versions[0].Fingerprint)
	}
}

// TestTheFirstSaveOfAFreshInstallationHasNoPreviousVersion is the case an installation
// really meets: config.json does not exist yet, and the rotation must not refuse.
func TestTheFirstSaveOfAFreshInstallationHasNoPreviousVersion(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "config.json")
	store := newStore(t, path)

	if err := store.Save(context.Background(), domain.NeutralProfile()); err != nil {
		t.Fatalf("premier enregistrement : %v", err)
	}
	if !fileExists(path) {
		t.Fatal("le premier enregistrement n'a rien écrit")
	}
	versions, err := store.Versions(context.Background())
	if err != nil {
		t.Fatalf("Versions : %v", err)
	}
	if len(versions) != 0 {
		t.Fatalf("%d version(s) après le premier enregistrement, attendu 0", len(versions))
	}
}

// TestReadSaysWhichFileItCannotRead is what a support call works from: a station has six
// of these files, and « JSON invalide » without a name sends a volunteer to the wrong one.
func TestReadSaysWhichFileItCannotRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	store := newStore(t, path)

	_, err := store.Read(context.Background())
	if err == nil {
		t.Fatal("un fichier absent a été lu")
	}
	if !strings.Contains(err.Error(), path) {
		t.Fatalf("le refus ne nomme pas le fichier : %v", err)
	}

	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatalf("écriture : %v", err)
	}
	_, err = store.Read(context.Background())
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("un JSON tronqué doit être refusé en nommant le fichier : %v", err)
	}
}

// TestTheWrittenFileIsReadableByAHumanBeing is not cosmetic: §11.1 keeps the
// configuration out of the database precisely so that somebody can open it in Notepad on
// a station whose base is corrupt.
func TestTheWrittenFileIsReadableByAHumanBeing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	store := newStore(t, path)
	if err := store.Save(context.Background(), domain.NeutralProfile()); err != nil {
		t.Fatalf("enregistrement : %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("relecture : %v", err)
	}
	if !strings.Contains(string(raw), "\n  \"station\"") {
		t.Fatalf("le fichier n'est pas indenté :\n%s", raw)
	}
	if !strings.HasSuffix(string(raw), "\n") {
		t.Fatal("le fichier ne finit pas par une fin de ligne")
	}
}

// TestAStoreWithoutAPathIsRefused catches the composition mistake where it happens, and
// not at the first save.
func TestAStoreWithoutAPathIsRefused(t *testing.T) {
	if _, err := NewConfigStore(""); err == nil {
		t.Fatal("un magasin de configuration sans chemin a été construit")
	}
}

// TestTheStoreNamesTheFileItWrites is what the page Poste displays: a volunteer on the
// telephone has to be able to read out where the configuration of this station lives (§14.4).
func TestTheStoreNamesTheFileItWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if got := newStore(t, path).Path(); got != path {
		t.Fatalf("Path() = %q, attendu %q", got, path)
	}
}

// --- Helpers ----------------------------------------------------------------

// newStore opens the store under test.
func newStore(t *testing.T, path string) *ConfigStore {
	t.Helper()
	store, err := NewConfigStore(path)
	if err != nil {
		t.Fatalf("NewConfigStore : %v", err)
	}
	return store
}

// writeStation writes one valid configuration and returns it.
//
// The NEUTRAL PROFILE and not a literal: it is the one configuration this binary ships,
// and a test that invented its own would prove nothing about the file a station writes.
func writeStation(t *testing.T, path string, number int) domain.Config {
	t.Helper()
	cfg := domain.NeutralProfile()
	cfg.Station.Number = number
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("sérialisation : %v", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("écriture : %v", err)
	}
	return cfg
}

// readConfig reads back what is on disk.
func readConfig(t *testing.T, path string) domain.Config {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("relecture de %s : %v", path, err)
	}
	var cfg domain.Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("%s illisible : %v", path, err)
	}
	return cfg
}

// fileExists reports whether a path is there.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// match lists what a glob finds in a directory.
func match(t *testing.T, directory, pattern string) []string {
	t.Helper()
	found, err := filepath.Glob(filepath.Join(directory, pattern))
	if err != nil {
		t.Fatalf("recherche %s : %v", pattern, err)
	}
	return found
}
