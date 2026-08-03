package diag

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
)

// This file carries the four controls that stand between a station and the place it
// writes: the rights on the data directory, the room left on its volume, the base itself,
// and the schema that base is at. They are ordered the way a failure travels — a directory
// nobody can write is a base nobody can open — and each remedy names the control above it
// rather than repeating its diagnosis.

// noDataDirectoryObserved is the fact the two directory controls report in the same words.
//
// They differ on the VERDICT and not on what was found: writing nowhere is a failure,
// measuring nowhere is merely unknowable.
const noDataDirectoryObserved = "aucun répertoire de données n'a été désigné"

// --- 4. The data directory --------------------------------------------------

func (d *Doctor) checkDataDirectory() Control {
	control := Control{ID: ControlDataDirectory,
		Checked: "Droits d'écriture sur le répertoire de données"}
	if d.o.DataDir == "" {
		control.Status, control.Observed = StatusFail, noDataDirectoryObserved
		control.Remedy = "Relancez la commande avec --data <répertoire>, ou renseignez OPENSCALE_DATA (§11.1)."
		return control
	}
	if err := probeWritable(d.o.DataDir); err != nil {
		control.Status = StatusFail
		control.Observed = fmt.Sprintf("impossible d'écrire dans %s : %v", d.o.DataDir, err)
		control.Remedy = writableRemedy(d.o.DataDir)
		return control
	}
	control.Status = StatusPass
	control.Observed = fmt.Sprintf("%s est accessible en écriture", d.o.DataDir)
	return control
}

// probeWritable proves the directory is writable BY WRITING.
//
// Reading the permission bits would answer a different question: on Windows an ACL that
// looks right can still be shadowed by an inherited deny, and on Linux a full or
// read-only mount grants the bits and refuses the write. The only honest test of « can
// this be written » is a write, and it removes what it wrote.
func probeWritable(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	probe, err := os.CreateTemp(dir, "doctor-*.tmp")
	if err != nil {
		return err
	}
	name := probe.Name()
	// The bytes matter: creating a file can succeed on a full volume, and only the write
	// then fails. A station whose disk is full must not be reported as writable.
	_, writeErr := probe.Write([]byte("openscale doctor\n"))
	closeErr := probe.Close()
	removeErr := os.Remove(name)
	return errors.Join(writeErr, closeErr, removeErr)
}

// writableRemedy names the command that fixes the rights, per system.
func writableRemedy(dir string) string {
	if runtime.GOOS == "windows" {
		return "Rendez le répertoire inscriptible au compte du service. En administrateur :\n" +
			`icacls "` + dir + `" /grant "SYSTEM:(OI)(CI)F" /T` + "\n" +
			"puis accordez la modification au compte du kiosque (§15.2, étape 2). Si le disque " +
			"est plein, c'est le contrôle 5 qui le dit."
	}
	return "Rendez le répertoire inscriptible au compte du service :\n" +
		"install -d -o openscale -g openscale " + dir + "\n" +
		"et vérifiez qu'il figure dans ReadWritePaths= de l'unité (§15.3)."
}

// --- 5. Disk space ----------------------------------------------------------

// codeDiskFull is ERR-SYS-05, which §15.4 allocates to a full disk and to the weighings
// it leaves unjournalled.
const codeDiskFull = "ERR-SYS-05"

func (d *Doctor) checkDiskSpace(loaded loadedConfig) Control {
	control := Control{ID: ControlDiskSpace, Checked: "Espace disque du répertoire de données"}
	if d.o.DataDir == "" {
		control.Status, control.Observed = StatusUnknown, noDataDirectoryObserved
		control.Remedy = "Relancez la commande avec --data <répertoire> (§11.1)."
		return control
	}
	space, err := d.o.Machine.FreeSpace(d.o.DataDir)
	if err != nil || !space.Determined {
		control.Status = StatusUnknown
		control.Observed = "l'espace libre du volume n'a pas pu être mesuré" + detailSuffix(err)
		control.Remedy = "Regardez l'espace libre du volume qui porte " + d.o.DataDir +
			" avec l'explorateur de fichiers ou avec df."
		return control
	}

	free, total := megabytes(space.FreeBytes), megabytes(space.TotalBytes)
	threshold := int64(loaded.Config.Maintenance.DiskAlertMB)
	control.Observed = fmt.Sprintf("%d Mo libres sur %d Mo", free, total)
	switch {
	case free <= 0:
		control.Status, control.Code = StatusFail, codeDiskFull
		control.Observed += " — le volume est plein : les pesées sortiront encore, et elles ne seront plus journalisées"
		control.Remedy = "Libérez de l'espace sur le volume qui porte " + d.o.DataDir + ". Les " +
			"copies de sauvegarde de la base (openscale.db.before-v… et .backup-…) sont les " +
			"plus gros fichiers qu'on puisse retirer sans rien perdre du journal."
	case threshold > 0 && free < threshold:
		control.Status, control.Code = StatusWarn, codeDiskFull
		control.Observed += fmt.Sprintf(" — sous le seuil d'alerte de %d Mo (maintenance.disk_alert_mb)", threshold)
		control.Remedy = "Libérez de l'espace avant que le journal ne cesse d'être écrit. Le seuil " +
			"lui-même se règle dans maintenance.disk_alert_mb."
	case threshold <= 0:
		control.Status = StatusPass
		control.Observed += " — aucun seuil d'alerte n'est déclaré (maintenance.disk_alert_mb)"
	default:
		control.Status = StatusPass
		control.Observed += fmt.Sprintf(" — seuil d'alerte %d Mo", threshold)
	}
	return control
}

// --- 8. The database --------------------------------------------------------

const codeDatabaseUnusable = "ERR-DB-01"

func (d *Doctor) checkDatabase(ctx context.Context, base Database, openErr error) Control {
	control := Control{ID: ControlDatabase, Checked: "Base ouvrable et contrôle d'intégrité"}
	if base == nil {
		control.Status = StatusFail
		control.Code, control.Observed = classifyDatabaseFailure(openErr)
		control.Remedy = "Le service ne démarrera pas sans la base. Vérifiez les droits du " +
			"répertoire de données (contrôle 4) et l'espace disque (contrôle 5). Si le fichier " +
			"est endommagé, restaurez la copie la plus récente rangée à côté de lui — " +
			"openscale.db.backup-… ou openscale.db.before-v… — et redémarrez le service (§15.5)."
		return control
	}
	if err := base.IntegrityCheck(ctx); err != nil {
		control.Status, control.Code = StatusFail, codeDatabaseUnusable
		control.Observed = fmt.Sprintf("%s s'ouvre, et son contrôle d'intégrité échoue : %v", base.Path(), err)
		control.Remedy = "Ne réparez rien à la main. Arrêtez le service, renommez le fichier, " +
			"restaurez la copie la plus récente (openscale.db.backup-… ou openscale.db.before-v…), " +
			"redémarrez le service (§15.5). Gardez le fichier endommagé : il porte les pesées " +
			"que la copie n'a pas."
		return control
	}
	control.Status = StatusPass
	control.Observed = fmt.Sprintf("%s s'ouvre et son contrôle d'intégrité passe", base.Path())
	return control
}

// classifyDatabaseFailure reports the code and the sentence of a refusal to open.
func classifyDatabaseFailure(err error) (code, observed string) {
	if err == nil {
		return codeDatabaseUnusable, "la base n'a pas pu être ouverte, sans raison rapportée"
	}
	var failure *DatabaseFailure
	if errors.As(err, &failure) && failure.Code != "" {
		return failure.Code, failure.Message
	}
	return codeDatabaseUnusable, "la base n'a pas pu être ouverte : " + err.Error()
}

// --- 9. Migrations ----------------------------------------------------------

const codeSchemaFromNewerVersion = "ERR-DB-02"

func (d *Doctor) checkMigrations(base Database, openErr error) Control {
	control := Control{ID: ControlMigrations, Checked: "Migrations à jour"}
	if base == nil {
		control.Status = StatusUnknown
		control.Observed = "la version du schéma n'est pas lisible parce que la base ne s'ouvre pas" +
			detailSuffix(openErr)
		control.Remedy = "Réglez d'abord le contrôle 8 : la version du schéma se lit dans la base."
		return control
	}
	applied, err := base.SchemaVersion()
	if err != nil {
		control.Status, control.Code = StatusFail, codeDatabaseUnusable
		control.Observed = "la version du schéma n'a pas pu être lue : " + err.Error()
		control.Remedy = "La base s'ouvre et ne répond pas : traitez-la comme endommagée et " +
			"restaurez la copie la plus récente (§15.5)."
		return control
	}
	switch {
	case d.o.Migrations <= 0:
		control.Status = StatusUnknown
		control.Observed = fmt.Sprintf("schéma %d appliqué ; le nombre de migrations que porte ce "+
			"binaire n'a pas été fourni à cette commande", applied)
		control.Remedy = "Rien à faire sur le poste : c'est cette commande qui n'a pas été câblée " +
			"complètement. Signalez-le."
	case applied > d.o.Migrations:
		control.Status, control.Code = StatusFail, codeSchemaFromNewerVersion
		control.Observed = fmt.Sprintf("la base est au schéma %d et ce binaire n'en connaît que %d : "+
			"elle a été créée par une version plus récente", applied, d.o.Migrations)
		control.Remedy = "Mettez l'application à jour sur ce poste. Si vous venez au contraire de " +
			"revenir en arrière volontairement, restaurez AUSSI la copie " +
			"openscale.db.before-v… correspondante : les migrations ne redescendent pas (§12.5)."
	case applied < d.o.Migrations:
		control.Status, control.Code = StatusFail, codeDatabaseUnusable
		control.Observed = fmt.Sprintf("la base est au schéma %d alors que ce binaire en porte %d : "+
			"les migrations n'ont pas été appliquées", applied, d.o.Migrations)
		control.Remedy = "Les migrations s'appliquent au démarrage du service. Démarrez-le " +
			"(contrôle 1) et relisez ce contrôle ; s'il reste rouge, la base est en lecture " +
			"seule ou le disque est plein (contrôles 4 et 5)."
	default:
		control.Status = StatusPass
		control.Observed = fmt.Sprintf("schéma %d, à jour", applied)
	}
	return control
}
