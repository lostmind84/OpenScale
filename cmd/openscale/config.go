package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"

	"openscale/internal/domain"
	"openscale/internal/platform"
)

// This file is the `config` subcommand of §15.1: which action runs, and the French
// sentences their refusals share. The two actions that only READ the file are in
// configread.go, the three that REWRITE it in configwrite.go.

// runConfig is `openscale config validate|export|fingerprint|password|recovery-code`
// (§15.1, §14.4).
//
// # Why `import` is still not here, and why `password` now is
//
// `import` is a gesture of the ADMINISTRATION SCREEN, and giving it a second home would
// give the station two ways of being reconfigured — one of them with no diff preview, no
// sixty-second confirmation and no fault list in front of a human (§11.4, §11.5).
//
// `password` was refused on that same argument, and the argument does not hold for it:
// there is no diff to preview and no rollback to arrange for one field, and above all a
// station whose configuration carries no password at all has NO screen to offer. The
// screen refuses to open a session (409), the recovery form has no code to check against,
// and `PUT /admin/api/config` is behind the very password nobody can set — a station out
// of the box was locked out of its own administration, which is the opposite of what
// ADR-018 arbitrates. §14.4 says it in one line: « `openscale config password` reste
// disponible en ligne de commande ».
//
// `recovery-code` is the other half of that line: §14.4 has the eight characters
// « générés à l'installation, imprimés sur la fiche », and install.ps1 has no way to
// produce an argon2id hash on its own.
func runConfig(args []string, in io.Reader, out io.Writer) error {
	fs := flag.NewFlagSet("config", flag.ContinueOnError)
	fs.SetOutput(out)
	var (
		hardware = fs.Bool("hardware", false,
			"conserver le bloc matériel dans l'export (par défaut il est retiré)")
		output = fs.String("output", "", "fichier de sortie de l'export ; sinon la sortie standard")
	)
	fs.Usage = func() { fmt.Fprint(out, configUsage) }

	positional, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	if len(positional) == 0 {
		fs.Usage()
		return errors.New("config prend une action : validate, export, fingerprint, migrate, " +
			"password ou recovery-code")
	}

	action := positional[0]
	path := platform.DefaultConfigPath()
	if len(positional) > 1 {
		path = positional[1]
	}
	if len(positional) > 2 {
		fs.Usage()
		return fmt.Errorf("argument inattendu %q : config prend une action et un fichier", positional[2])
	}

	cfg, notes, decodeFaults, err := readConfigLeniently(path)
	if err != nil {
		return fmt.Errorf("le fichier de configuration %s ne peut pas être lu : %w", path, err)
	}

	switch action {
	case "validate":
		return validateConfig(out, path, cfg, notes, decodeFaults)
	case "export":
		if err := refuseWhatWasNotRead(path, decodeFaults,
			"l'export emporterait la configuration d'usine %s vers les autres postes"); err != nil {
			return err
		}
		return exportConfig(out, cfg, *hardware, *output)
	case "fingerprint":
		if err := refuseWhatWasNotRead(path, decodeFaults,
			"l'empreinte porterait sur la configuration d'usine %s, et les huit "+
				"caractères que les postes comparent ne diraient plus rien de ce fichier"); err != nil {
			return err
		}
		fmt.Fprintf(out, "%s\n", cfg.Fingerprint())
		return nil
	case "migrate":
		return migrateConfig(out, path)
	case "password":
		return setAdminPassword(in, out, path)
	case "recovery-code":
		return mintRecoveryCode(out, path)
	}
	fs.Usage()
	return fmt.Errorf("action inconnue %q : validate, export, fingerprint, migrate, password "+
		"ou recovery-code", action)
}

const configUsage = `Usage : openscale config <action> [fichier] [options]

Sans fichier, c'est la configuration de ce poste qui est lue.

Actions :
  validate        liste TOUTES les fautes en français, d'un coup
  export          écrit la configuration à cloner vers les autres postes
  fingerprint     affiche l'empreinte de 8 caractères des réglages partagés
  migrate         remet le fichier à la forme que ce binaire lit, et dit ce qu'il change
  password        pose le mot de passe d'administration, lu sur l'entrée standard
  recovery-code   tire le code de secours de 8 caractères et l'affiche UNE fois

Options d'export :
  --hardware            conserver le bloc matériel (numéro de poste, port série, file
                        d'impression, adresse d'écoute). Par défaut il est RETIRÉ :
                        c'est ce qu'un poste cloné ne doit pas hériter
  --output <fichier>    écrire dans un fichier plutôt que sur la sortie standard

Le mot de passe d'administration ne sort JAMAIS, avec ou sans --hardware.
Importer une configuration se fait depuis l'écran d'administration : l'aperçu du diff
champ par champ et la confirmation de 60 secondes en font partie.

migrate, password et recovery-code écrivent le FICHIER, que le poste ne relit qu'au
démarrage : arrêtez le service, lancez la commande, redémarrez-le. Le terminal affiche ce
qui est tapé — c'est une console de poste, pas un poste de travail partagé.
`

// refuseWhatWasNotRead stops an action that would ANSWER ABOUT a file this binary did not
// read whole.
//
// `fingerprint` and `export` are the two, and neither has any way of saying « sauf ce
// bloc-là ». One produces the eight characters four stations of a cooperative compare BY
// EYE to know they share a configuration (ADR-012, §11.4); the other produces the FILE
// those stations are configured from (§11.5). A block that fell back on the neutral
// profile makes the first answer about a configuration NOBODY DECLARED — measured, 428807b3
// becomes 7b386ddb in silence — and makes the second CLONE it onto the three others. That
// is the failure `config migrate` was just stopped from writing, propagated by the copying
// instead.
//
// `validate` is not in this list and must not be: its job is precisely to NAME the faults,
// so it reports them rather than refusing to look.
//
// consequence carries ONE %s, filled with the possessive agreed with the number of blocks:
// « à sa place » for one, « à leur place » for two, which is the ordinary case of an old
// file whose two fields changed type.
func refuseWhatWasNotRead(path string, faults []domain.Fault, consequence string) error {
	if len(faults) == 0 {
		return nil
	}
	unreadable := &domain.UnreadableBlocksError{Faults: faults}
	return &serviceFailure{Exit: exitFailure, Message: fmt.Sprintf("%s : %s. %s",
		unreadablePart(path, faults), fmt.Sprintf(consequence, unreadable.InTheirPlace()),
		wayOut(path))}
}

// unreadablePart names what did not decode AND the file it was in, in French, agreed in
// number.
//
// The agreement itself is not decided here: domain.UnreadableBlocksError owns it, so that
// this sentence and the four the administration screen writes cannot come to disagree —
// which they already had, « les blocs pricing, catalog ne s'y lit pas ». The two cases
// §11.3 keeps apart stay apart: ONE block of an otherwise sound file is repaired in place,
// a document that is not JSON at all is restored from config.json.1.
func unreadablePart(path string, faults []domain.Fault) string {
	unreadable := &domain.UnreadableBlocksError{Faults: faults}
	if unreadable.Names(domain.WholeDocumentField) {
		return fmt.Sprintf("%s n'est pas un document JSON exploitable", path)
	}
	return fmt.Sprintf("%s de %s %s", unreadable.BlockPhrase(), path, unreadable.NotRead())
}

// wayOut names the one gesture that gets a station out of this, by the BASE NAME of the
// backup and not by its whole path.
//
// The path is already in the sentence this follows, and a station's config.json sits at
// C:\ProgramData\OpenScale — printing it twice made the useful half of a message a
// volunteer has to read scroll off a console line. Naming the file they will look for,
// beside the one they already know about, says the same thing in eight characters.
func wayOut(path string) string {
	return fmt.Sprintf("Corrigez-le, ou repartez de %s, la version d'avant rangée à côté",
		filepath.Base(path)+".1")
}

// readFailure is the WHOLE sentence, in French, saying why a configuration file could not
// be read — the path exactly once, and nothing in English at the end of it.
//
// It exists because `%w` on a typed error puts its Error() — English, by Go convention —
// at the end of a phrase a volunteer is reading. Measured on `openscale config password`
// before this: « le fichier de configuration …/config.json ne peut pas être lu :
// …/config.json : le bloc pricing n'a pas pu être lu, … : domain: config block(s) did not
// decode: pricing ». The path twice, and the last clause in another language.
func readFailure(path string, err error) string {
	var unreadable *domain.UnreadableBlocksError
	if errors.As(err, &unreadable) {
		return fmt.Sprintf("%s, et ce qui en tient lieu est la configuration d'usine. %s",
			unreadablePart(path, unreadable.Faults), wayOut(path))
	}
	return fmt.Sprintf("le fichier de configuration %s ne peut pas être lu : %v", path, err)
}
