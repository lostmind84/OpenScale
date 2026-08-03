package diag

import (
	"fmt"
	"strings"

	"openscale/internal/domain"
)

// This file carries the two controls about what a station was TOLD: the configuration
// file, and the catalog source that file declares. They are together because they share
// one rule — a fault here is never a fault of the machine, and every remedy names the
// screen or the command that rewrites the document rather than a cable to check.

// --- 7. The configuration ---------------------------------------------------

// codeFactoryConfig is ERR-CFG-01: the station runs on the neutral profile because its
// configuration did not pass (§11.3).
const codeFactoryConfig = "ERR-CFG-01"

func (d *Doctor) checkConfiguration(loaded loadedConfig) Control {
	control := Control{ID: ControlConfiguration, Checked: "Configuration valide"}
	switch {
	case !loaded.Present:
		control.Status = StatusFail
		control.Observed = fmt.Sprintf("le fichier %s ne peut pas être lu : %v",
			or(d.o.ConfigPath, "de configuration"), loaded.Err)
		control.Remedy = "Le service ne démarrera pas sans lui. Vérifiez le chemin (--config, " +
			"OPENSCALE_CONFIG, ou l'emplacement par défaut de §11.1) et les droits de lecture. " +
			"Si le fichier a disparu, restaurez-en une des cinq versions rangées à côté de lui " +
			"(config.json.1 à .5)."
		return control
	case !loaded.Parsed:
		control.Status, control.Code = StatusFail, codeFactoryConfig
		control.Observed = fmt.Sprintf("%s n'est pas un JSON exploitable (%v) — le poste tourne "+
			"quand même, en configuration d'usine, et ne calcule aucun prix ; l'écran "+
			"d'administration répond", d.o.ConfigPath, loaded.Err)
		control.Remedy = "Corrigez la faute de syntaxe — c'est presque toujours une virgule en " +
			"trop avant une accolade — ou restaurez config.json.1, la version précédente " +
			"rangée à côté du fichier (§11.4)."
		return control
	case len(loaded.Faults) > 0:
		control.Status, control.Code = StatusFail, codeFactoryConfig
		control.Observed = fmt.Sprintf("%d faute(s) — le poste démarre en configuration d'usine et ne "+
			"calcule aucun prix. %s", len(loaded.Faults), faultSummary(loaded.Faults))
		control.Remedy = "Corrigez les fautes ci-dessus dans " + d.o.ConfigPath + ", ou restaurez une " +
			"version précédente depuis l'écran d'administration (§11.4). " +
			"`openscale config validate " + d.o.ConfigPath + "` les liste TOUTES, d'un coup."
		return control
	}

	// A configuration with no fault is only FULLY checked when this command was given the
	// registries the file names its drivers in: §11.3 validates the form without them, and
	// announcing « aucune faute » on a half-checked file would be a claim nobody made.
	if missing := unknownDrivers(loaded.Config, d.o.Registries); len(missing) > 0 {
		control.Status = StatusUnknown
		control.Observed = fmt.Sprintf("aucune faute de forme, et les drivers nommés par le fichier "+
			"n'ont pas pu être vérifiés faute de registre : %s", strings.Join(missing, " · "))
		control.Remedy = "Relancez `openscale config validate " + d.o.ConfigPath + "` : la commande de " +
			"§15.1 porte les registres de ce binaire et liste toutes les fautes d'un coup."
		return control
	}

	// A station with no administration password WEIGHS — that is the whole point of not
	// making it a fault (ADR-033) — but nothing else would say so, and « rien ne le dit »
	// is exactly how a station ended up locked out of its own settings: the delivered
	// file carried a placeholder hash, `config validate` declared it sound, and the
	// installation sheet went out with dotted lines. This is a WARNING and never a
	// failure: the way in exists, it is the recovery code, and saying where it is written
	// is more use to a volunteer than a red line.
	if loaded.Config.Admin.PasswordHash == "" {
		control.Status = StatusWarn
		control.Observed = "aucune faute, et aucun mot de passe d'administration n'est posé : " +
			"les réglages s'ouvrent en lecture, mais rien ne peut être enregistré"
		control.Remedy = "Posez-en un depuis l'écran d'administration, avec le code de secours " +
			"de la fiche d'installation, ou en ligne de commande : `openscale config password " +
			d.o.ConfigPath + "`."
		return control
	}

	if retired := loaded.Config.Retired(); len(retired) > 0 {
		control.Status = StatusWarn
		control.Observed = fmt.Sprintf("aucune faute, et %d clé(s) retirée(s) traînent encore dans le "+
			"fichier : %s", len(retired), strings.Join(retired, ", "))
		control.Remedy = "Lancez d'abord « openscale config migrate " + d.o.ConfigPath + " » : il migre " +
			"tout seul ce qui se convertit, et détaille pourquoi il refuse le reste. Ce qu'il refuse ne " +
			"se devine pas ; retirez ces lignes-là à la main du fichier, puis relancez la migration (§11.2)."
		return control
	}

	// The schema version, because "this station's file was rewritten by the update" and
	// "this station's file is only being read as if it were" are two different states, and
	// diagnostic.zip is where somebody decides which one they are looking at. It is placed
	// LAST among the warnings and never among the faults: the station already runs on the
	// migrated form, in memory, so an out-of-date FILE is at most something to catch up on
	// — and it must never bury the two warnings above, which both call for action sooner
	// (no way in at all, or lines nobody can explain).
	//
	// A note is not automatically "behind, and migrate catches it up": migrateConfig
	// refuses to write ANYTHING while a single note is MigrationRefused (cmd/openscale/
	// config.go), so promising a rewrite on the strength of len(notes) alone would be
	// wrong exactly when it matters — a refused note is never routine. One refusal in
	// particular is not even an old file: a note on domain.SchemaVersionKey is what a
	// ROLLED-BACK station looks like from here, written by a binary NEWER than this one,
	// and it earns its own sentence rather than being folded into "des changements".
	if notes := loaded.MigrationNotes; len(notes) > 0 {
		control.Status = StatusWarn
		var refused []domain.MigrationNote
		var rolledBack *domain.MigrationNote
		for i := range notes {
			if notes[i].Action != domain.MigrationRefused {
				continue
			}
			refused = append(refused, notes[i])
			if notes[i].Key == domain.SchemaVersionKey {
				rolledBack = &notes[i]
			}
		}

		switch {
		case rolledBack != nil:
			control.Observed = fmt.Sprintf("aucune faute ; empreinte %s ; %s : %s",
				loaded.Config.Fingerprint(), d.o.ConfigPath, rolledBack.Message)
			control.Remedy = "Ce n'est pas un fichier en retard : cherchez pourquoi ce poste tourne " +
				"sur un binaire plus ancien qu'il ne l'a fait — les journaux de mise à jour " +
				"(update.ps1 ou update.sh) sur CE poste disent ce qui a échoué. « openscale config " +
				"migrate " + d.o.ConfigPath + " » ne réécrira rien tant que ce fichier vient d'un " +
				"binaire plus récent."
		// Unreachable TODAY, and kept because it is the right welcome for the first refusal
		// that is not one of retiredKeys. Every refusal this binary can produce on a key
		// other than `version` LEAVES THAT KEY IN THE DOCUMENT -- that is what a refusal
		// consists of (ADR-058) -- so Config.Retired() finds it and the branch above returns
		// first. Only `version` reaches a refusal with nothing left behind, and it has its
		// own case, right above this one.
		case len(refused) > 0:
			control.Observed = fmt.Sprintf("aucune faute ; empreinte %s ; %s porte %d changement(s) "+
				"que ce binaire ne convertit pas — « openscale config migrate » les nommera, chacun "+
				"avec sa raison, mais n'écrira RIEN tant qu'ils y restent",
				loaded.Config.Fingerprint(), d.o.ConfigPath, len(refused))
			control.Remedy = "Lancez « openscale config migrate " + d.o.ConfigPath + " » pour lire la " +
				"raison de chaque point refusé, tranchez-les à la main, puis relancez la commande : " +
				"elle n'écrit le fichier qu'une fois qu'il n'y en a plus."
		default:
			control.Observed = fmt.Sprintf("aucune faute ; empreinte %s ; %s n'est pas encore au schéma %d "+
				"que ce binaire écrit (%d changement(s) en attente) — « openscale config migrate » le "+
				"réécrit (le poste tourne déjà sur la forme à jour, en mémoire)",
				loaded.Config.Fingerprint(), d.o.ConfigPath, domain.CurrentSchemaVersion, len(notes))
			control.Remedy = "« openscale config migrate " + d.o.ConfigPath + " » réécrit le fichier sur " +
				"cette forme ; rien ne presse, le poste fonctionne déjà normalement."
		}
		return control
	}

	control.Status = StatusPass
	control.Observed = fmt.Sprintf("aucune faute ; empreinte %s", loaded.Config.Fingerprint())
	return control
}

// unknownDrivers names the drivers the file declares and the registries do not carry.
//
// It is what tells « the configuration is valid » from « the configuration has no fault this
// command was able to look for ». The scale is only checked when the station declares one:
// scale.type is legitimately empty on a station that has no scale (§11.2).
func unknownDrivers(cfg domain.Config, reg domain.Registries) []string {
	var missing []string
	declared := []struct {
		field string
		value string
		known []string
		check bool
	}{
		{"scale.type", cfg.Scale.Type, reg.ScaleTypes(), cfg.Scale.Present},
		{"printer.type", cfg.Printer.Type, reg.PrinterTypes(), true},
		{"catalog.type", cfg.Catalog.Type, reg.CatalogSourceNames(), cfg.Catalog.Type != ""},
	}
	for _, entry := range declared {
		if !entry.check || known(entry.known, entry.value) {
			continue
		}
		missing = append(missing, entry.field)
	}
	return missing
}

// known reports whether value is in list.
func known(list []string, value string) bool {
	for _, candidate := range list {
		if candidate == value {
			return true
		}
	}
	return false
}

// faultsQuoted is how many faults the one-line summary names before deferring to
// `openscale config validate`. Three: enough to recognise the block that is wrong, short
// enough to stay on a terminal line a volunteer reads out over the telephone.
const faultsQuoted = 3

// faultSummary names the first faults and says how many were left out.
func faultSummary(faults []domain.Fault) string {
	quoted := faults
	if len(quoted) > faultsQuoted {
		quoted = quoted[:faultsQuoted]
	}
	parts := make([]string, 0, len(quoted))
	for _, fault := range quoted {
		// faultLine and not Fault.String: the message of a fault about a sensitive field
		// quotes the offending VALUE, and this sentence travels into diagnostic.zip.
		parts = append(parts, faultLine(fault))
	}
	out := strings.Join(parts, " · ")
	if len(faults) > len(quoted) {
		out += fmt.Sprintf(" · et %d autre(s)", len(faults)-len(quoted))
	}
	return out
}

// --- 13. The catalog source, as the service sees it -------------------------

const codeCatalogSource = "ERR-CAT-01"

func (d *Doctor) checkCatalogSource(loaded loadedConfig, health Health, healthErr error) Control {
	control := Control{ID: ControlCatalogSource,
		Checked: "Source du catalogue accessible telle que le service la voit"}
	if healthErr != nil {
		control.Status = StatusUnknown
		control.Observed = "le service ne répond pas, et lui seul voit la source avec SES droits. " +
			d.declaredSourceSentence(loaded)
		control.Remedy = "Démarrez le service (contrôle 1), puis relancez openscale doctor. Ce " +
			"contrôle passe par le service exprès : vérifier le répertoire avec les droits de " +
			"l'opérateur répondrait à une autre question (§15.4)."
		return control
	}

	weighable := health.State.CatalogCount
	last := health.Catalog
	switch {
	case last == nil && weighable == 0:
		control.Status, control.Code = StatusWarn, codeCatalogSource
		control.Observed = "le service n'a encore appliqué aucun catalogue et ne sert aucun produit " +
			"pesable. " + d.declaredSourceSentence(loaded)
		control.Remedy = catalogArrivalRemedy(loaded)
	case last != nil && last.Result == domain.ImportRejected:
		control.Status, control.Code = StatusWarn, or(last.Code, codeCatalogSource)
		control.Observed = fmt.Sprintf("le dernier fichier lu a été REFUSÉ (%s, %s) : %s. Le catalogue "+
			"précédent reste en service, %d produits pesables",
			last.Source, or(last.FileName, "sans nom"), or(last.Reason, "sans motif"), weighable)
		control.Remedy = "Le poste continue de peser avec le catalogue précédent : rien n'est perdu. " +
			"Ouvrez la page Catalogue : les lignes fautives y sont nommées, avec leur " +
			"numéro de ligne dans le CSV, et c'est cette liste-là qu'il faut envoyer au producteur."
	case last != nil && last.Result == domain.ImportFailed:
		control.Status, control.Code = StatusWarn, or(last.Code, codeCatalogSource)
		control.Observed = fmt.Sprintf("le dernier import a échoué (%s, %s) : %s. %d produits pesables "+
			"restent en service", last.Source, or(last.FileName, "sans nom"),
			or(last.Reason, "sans motif"), weighable)
		control.Remedy = "Regardez le journal technique de l'écran d'administration : un échec " +
			"d'import est un problème d'accès ou de droits sur la source, pas de contenu. " +
			d.declaredSourceSentence(loaded)
	case weighable == 0:
		control.Status, control.Code = StatusWarn, codeCatalogSource
		control.Observed = fmt.Sprintf("le dernier import a réussi (%s, %s) et le service ne sert "+
			"aucun produit pesable", last.Source, or(last.FileName, "sans nom"))
		control.Remedy = "La grille du client est vide. Vérifiez sur la page Catalogue " +
			"que les produits reçus portent bien un code-barres commençant par 0493 à 0499 : " +
			"c'est le préfixe qui décide si un produit se pèse."
	default:
		control.Status = StatusPass
		control.Observed = fmt.Sprintf("%d produits pesables en service ; dernier fichier appliqué : "+
			"%s via %s (%d lignes lues, %d anomalies)", weighable, or(last.FileName, "sans nom"),
			last.Source, last.RowsRead, last.Anomalies)
	}
	return control
}

// declaredSourceSentence names the source the FILE declares, labelled as declared.
//
// It never claims the directory was tested: that claim belongs to the service, and this
// sentence exists precisely for the case where the service cannot make it.
func (d *Doctor) declaredSourceSentence(loaded loadedConfig) string {
	kind := loaded.Config.Catalog.Type
	if kind == "" {
		return "Aucune source n'est déclarée dans catalog.type."
	}
	if kind == domain.CatalogSourceWebDAV {
		// The URL is deliberately NOT quoted here: this sentence travels into
		// diagnostic.zip, and §15.4 wants that archive free of anything private.
		return "Source déclarée : webdav (l'adresse n'est pas reproduite ici)."
	}
	return fmt.Sprintf("Source déclarée : %s ; le service crée lui-même son répertoire de dépôt "+
		"sous %s.", kind, or(d.o.DataDir, "le répertoire de données"))
}

// catalogArrivalRemedy names the file the station is waiting for.
//
// The name DERIVES from station.number and is never written by hand: §14.4 makes that a
// rule, because two declarations of one fact is the failure the legacy application died
// of.
func catalogArrivalRemedy(loaded loadedConfig) string {
	expected := "flv_<numéro de poste>.csv"
	if loaded.Config.Station.Number > 0 {
		expected = fmt.Sprintf("flv_%d.csv", loaded.Config.Station.Number)
	}
	return "La grille du client est vide et affiche « Catalogue vide ». Faites déposer " + expected +
		" par le producteur, ou glissez un CSV dans l'écran de dépannage → « Importer un " +
		"catalogue » : c'est le même parseur et la même qualification."
}
