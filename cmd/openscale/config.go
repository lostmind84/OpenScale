package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"openscale/internal/domain"
	"openscale/internal/platform"
	"openscale/internal/printing/transport"
)

// runConfig is `openscale config validate|export|fingerprint` (§15.1).
//
// # Why these three and not the five of §15.1
//
// `import` and `password` are gestures of the ADMINISTRATION SCREEN, and giving them a
// second home would give the station two ways of being reconfigured — one of them with
// no diff preview, no sixty-second confirmation and no fault list in front of a human
// (§11.4, §11.5). What a terminal is genuinely better at is the three below: telling a
// volunteer on the telephone what is wrong with a file, producing the hardware-free
// export the other three stations are cloned from, and reading out the eight characters
// that say the fleet is homogeneous.
func runConfig(args []string, out io.Writer) error {
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
		return errors.New("config prend une action : validate, export ou fingerprint")
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

	cfg, err := readConfigLeniently(path)
	if err != nil {
		return fmt.Errorf("le fichier de configuration %s ne peut pas être lu : %w", path, err)
	}

	switch action {
	case "validate":
		return validateConfig(out, path, cfg)
	case "export":
		return exportConfig(out, cfg, *hardware, *output)
	case "fingerprint":
		fmt.Fprintf(out, "%s\n", cfg.Fingerprint())
		return nil
	}
	fs.Usage()
	return fmt.Errorf("action inconnue %q : validate, export ou fingerprint", action)
}

const configUsage = `Usage : openscale config <validate|export|fingerprint> [fichier] [options]

Sans fichier, c'est la configuration de ce poste qui est lue.

Actions :
  validate      liste TOUTES les fautes en français, d'un coup
  export        écrit la configuration à cloner vers les autres postes
  fingerprint   affiche l'empreinte de 8 caractères des réglages partagés

Options d'export :
  --hardware            conserver le bloc matériel (numéro de poste, port série, file
                        d'impression, adresse d'écoute). Par défaut il est RETIRÉ :
                        c'est ce qu'un poste cloné ne doit pas hériter
  --output <fichier>    écrire dans un fichier plutôt que sur la sortie standard

Le mot de passe d'administration ne sort JAMAIS, avec ou sans --hardware.
Importer une configuration et changer le mot de passe se font depuis l'écran
d'administration : l'aperçu du diff champ par champ et la confirmation de 60 secondes
en font partie.
`

// validateConfig runs the controls of §11.3 with the REAL registries of this binary,
// and prints every fault at once.
//
// Every fault and not the first: a volunteer who came to fix one file should leave
// having fixed it, and not discover the second fault after a restart. The exit code is
// what makes it usable from install.ps1 — a non-zero status means « this station will
// start in factory configuration ».
func validateConfig(out io.Writer, path string, cfg domain.Config) error {
	scales, printers := scaleRegistry(), printerRegistry()
	faults := cfg.Validate(domain.Registries{
		Scales:         scales.Descriptors(),
		Printers:       printers.Descriptors(),
		Transports:     transport.Descriptors(),
		CatalogSources: catalogSourceDescriptors(),
	})
	if len(faults) == 0 {
		fmt.Fprintf(out, "%s : aucune faute. Empreinte des réglages partagés : %s\n",
			path, cfg.Fingerprint())
		return nil
	}
	fmt.Fprintf(out, "%s : %d faute(s).\n", path, len(faults))
	for _, fault := range faults {
		fmt.Fprintf(out, "  %s\n", fault.String())
	}
	return &serviceFailure{Exit: exitFailure, Message: fmt.Sprintf(
		"%s comporte %d faute(s) : le poste démarrerait en configuration d'usine (ERR-CFG-01)",
		path, len(faults))}
}

// exportConfig writes what §11.5 clones.
//
// It is the SAME domain.Config.Export the administration route calls, and it has to be:
// two exports that differed by a field would produce two fingerprints, and the eight
// characters four volunteers compare by eye would stop meaning anything.
func exportConfig(out io.Writer, cfg domain.Config, hardware bool, output string) error {
	exported := cfg.Export(hardware)
	// The recovery code is printed on the installation sheet OF ONE STATION. Carrying it
	// into a clone is the « four stations sharing one secret nobody chose » that Export
	// already refuses for the password, and the administration route redacts it here too.
	exported.Admin.RecoveryCodeHash = ""

	raw, err := json.MarshalIndent(exported, "", "  ")
	if err != nil {
		return fmt.Errorf("l'export n'a pas pu être encodé : %w", err)
	}
	raw = append(raw, '\n')

	if output == "" {
		_, err = out.Write(raw)
		return err
	}
	if err := os.WriteFile(output, raw, 0o644); err != nil {
		return fmt.Errorf("l'export n'a pas pu être écrit dans %s : %w", output, err)
	}
	fmt.Fprintf(out, "export écrit dans %s — empreinte %s\n", output, cfg.Fingerprint())
	return nil
}
