package diag

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The tests of doctor_storage.go: the four controls that stand between a station and the
// place it writes — the rights on the data directory, the room left on its volume, the
// base, and the schema that base is at. The write-rights case writes for real, on a real
// temporary directory: a double that pretended to would test nothing.

// --- 4. The data directory --------------------------------------------------

func TestADataDirectoryThatCannotBeWrittenIsProvedByWriting(t *testing.T) {
	b := newBench(t)
	// A regular FILE where a directory is expected: MkdirAll refuses it on every system,
	// including Windows, where marking a directory read-only does not stop a write.
	blocker := filepath.Join(b.dataDir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("préparation du bloqueur : %v", err)
	}
	b.dataDir = filepath.Join(blocker, "data")

	found := control(t, b.run(), ControlDataDirectory)
	if found.Status != StatusFail {
		t.Fatalf("répertoire inutilisable : %s", found.Status)
	}
	if !strings.Contains(found.Remedy, b.dataDir) {
		t.Errorf("la consigne doit nommer le répertoire :\n%s", found.Remedy)
	}
}

// --- 5. Disk space ----------------------------------------------------------

func TestAFullDiskIsRedAndALowDiskIsAmber(t *testing.T) {
	full := newBench(t)
	full.machine.space.FreeBytes = 0
	found := control(t, full.run(), ControlDiskSpace)
	if found.Status != StatusFail || found.Code != "ERR-SYS-05" {
		t.Fatalf("disque plein : %s / %q, attendu ÉCHEC / ERR-SYS-05", found.Status, found.Code)
	}
	if !strings.Contains(found.Observed, "journalisées") {
		t.Errorf("le constat doit dire ce qu'un disque plein coûte : les pesées sortent et ne sont "+
			"plus journalisées (ADR-013) :\n%s", found.Observed)
	}

	low := newBench(t)
	// The threshold is the one the configuration declares, and nothing else: 200 Mo in the
	// neutral profile. 32 Mo is under it, and the volume is not full.
	low.machine.space.FreeBytes = 32 << 20
	found = control(t, low.run(), ControlDiskSpace)
	if found.Status != StatusWarn {
		t.Fatalf("sous le seuil d'alerte : %s, attendu ATTENTION", found.Status)
	}
	if !strings.Contains(found.Observed, "200 Mo") {
		t.Errorf("le constat doit citer le seuil déclaré :\n%s", found.Observed)
	}
}

func TestASpaceThatCouldNotBeMeasuredIsNeverReportedAsZero(t *testing.T) {
	b := newBench(t)
	b.machine.space = FreeSpace{}
	b.machine.spaceErr = errors.New("volume inaccessible")

	found := control(t, b.run(), ControlDiskSpace)
	if found.Status != StatusUnknown {
		t.Fatalf("mesure impossible : %s, attendu INCONNU — un chiffre que personne n'a mesuré "+
			"enverrait quelqu'un supprimer des fichiers", found.Status)
	}
	if strings.Contains(found.Observed, "0 Mo") {
		t.Errorf("le constat ne doit citer aucun chiffre :\n%s", found.Observed)
	}
}

// --- 8. The database --------------------------------------------------------

func TestABaseThatWillNotOpenNamesItsCode(t *testing.T) {
	b := newBench(t)
	b.openErr = &DatabaseFailure{Code: "ERR-DB-01",
		Message: "ouverture de openscale.db impossible : accès refusé"}

	report := b.run()
	found := control(t, report, ControlDatabase)
	if found.Status != StatusFail || found.Code != "ERR-DB-01" {
		t.Fatalf("base fermée : %s / %q", found.Status, found.Code)
	}
	// The migration control cannot conclude without the base, and it says so rather than
	// accusing the schema.
	migrations := control(t, report, ControlMigrations)
	if migrations.Status != StatusUnknown {
		t.Errorf("migrations sans base : %s, attendu INCONNU", migrations.Status)
	}
	if !strings.Contains(migrations.Remedy, "contrôle 8") {
		t.Errorf("la consigne doit renvoyer au contrôle qui bloque :\n%s", migrations.Remedy)
	}
}

func TestADamagedBaseIsNeverRepairedByThisCommand(t *testing.T) {
	b := newBench(t)
	b.base.integrityErr = errors.New("row 12 missing from index products_by_category")

	found := control(t, b.run(), ControlDatabase)
	if found.Status != StatusFail {
		t.Fatalf("base endommagée : %s", found.Status)
	}
	if !strings.Contains(found.Remedy, "restaurez") || !strings.Contains(found.Remedy, "Gardez") {
		t.Errorf("la consigne doit dire de restaurer ET de garder le fichier endommagé, qui porte "+
			"les pesées que la copie n'a pas :\n%s", found.Remedy)
	}
}

func TestTheBaseIsGivenBackAtTheEndOfTheRun(t *testing.T) {
	b := newBench(t)
	b.run()
	if !b.base.closed {
		t.Error("la base n'a pas été refermée : le SERVICE la possède le reste du temps")
	}
}

// --- 9. Migrations ----------------------------------------------------------

func TestABaseFromANewerVersionIsErrDb02AndSaysToUpdateTheBinary(t *testing.T) {
	b := newBench(t)
	b.base.schema = 9
	b.migrations = 1

	found := control(t, b.run(), ControlMigrations)
	if found.Status != StatusFail || found.Code != "ERR-DB-02" {
		t.Fatalf("schéma plus récent : %s / %q", found.Status, found.Code)
	}
	if !strings.Contains(found.Observed, "9") || !strings.Contains(found.Observed, "1") {
		t.Errorf("le constat doit citer LES DEUX numéros :\n%s", found.Observed)
	}
	if !strings.Contains(found.Remedy, "before-v") {
		t.Errorf("la consigne doit dire qu'un retour arrière restaure aussi la copie, puisque les "+
			"migrations ne redescendent pas :\n%s", found.Remedy)
	}
}

func TestMigrationsNotAppliedPointAtTheServiceThatAppliesThem(t *testing.T) {
	b := newBench(t)
	b.base.schema = 0
	b.migrations = 3

	found := control(t, b.run(), ControlMigrations)
	if found.Status != StatusFail {
		t.Fatalf("migrations en retard : %s", found.Status)
	}
	if !strings.Contains(found.Remedy, "démarrage") && !strings.Contains(found.Remedy, "Démarrez") {
		t.Errorf("la consigne doit dire que les migrations s'appliquent au démarrage :\n%s", found.Remedy)
	}
}

func TestAMigrationCountNobodySuppliedIsNotGuessed(t *testing.T) {
	b := newBench(t)
	b.migrations = 0

	found := control(t, b.run(), ControlMigrations)
	if found.Status != StatusUnknown {
		t.Fatalf("nombre de migrations non fourni : %s, attendu INCONNU", found.Status)
	}
	if !strings.Contains(found.Remedy, "Signalez") {
		t.Errorf("la consigne doit dire qu'il n'y a rien à faire sur le poste :\n%s", found.Remedy)
	}
}
