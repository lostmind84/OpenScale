package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"openscale/internal/domain"
	"openscale/internal/printing/transport"
)

// This file holds the `config` actions that never modify the station's own file:
// validate names every fault at once, export writes what §11.5 clones onto the other
// stations. Both only READ config.json — the three that rewrite it are in
// configwrite.go.

// validateConfig runs the controls of §11.3 with the REAL registries of this binary,
// and prints every fault at once.
//
// Every fault and not the first: a volunteer who came to fix one file should leave
// having fixed it, and not discover the second fault after a restart. The exit code is
// what makes it usable from install.ps1 — a non-zero status means « this station will
// start in factory configuration ».
//
// That promise is only true because the DECODING faults are counted here too. A block
// that will not decode falls back on the neutral profile, and the substitute passes
// Validate without a word: judging on Validate alone answered « aucune faute » about a
// station that comes up in ERR-CFG-01, while serve — reading the very same file through
// the very same door — reported it.
func validateConfig(out io.Writer, path string, cfg domain.Config,
	notes []domain.MigrationNote, decodeFaults []domain.Fault) error {
	reportPendingMigrations(out, path, notes)

	scales, printers := scaleRegistry(), printerRegistry()
	// The decoding faults FIRST, and the concatenation is the one serve.go already makes:
	// a block that was replaced is what makes every value below it suspect, so it is read
	// before the judgements passed on those values.
	faults := append(decodeFaults, cfg.Validate(domain.Registries{
		Scales:         scales.Descriptors(),
		Printers:       printers.Descriptors(),
		Transports:     transport.Descriptors(),
		CatalogSources: catalogSourceDescriptors(),
	})...)
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

// reportPendingMigrations names what LoadConfig had to change to bring the file up to the
// schema this binary speaks, BEFORE the fault list: a volunteer reading a fault about a
// field they never touched should learn first that the field came from an old file, not
// discover it after wondering why the value looks wrong.
//
// It says NOTHING when there is nothing pending, on purpose: a station already at this
// schema must not see a paragraph on every `config validate`, only the ones that changed
// something. A retired key Migrate has no translation for (the six of the numbering plan)
// earns no note here either — control 20 names it in the fault list right below, which is
// where a pure refusal has always been reported.
func reportPendingMigrations(out io.Writer, path string, notes []domain.MigrationNote) {
	if len(notes) == 0 {
		return
	}
	fmt.Fprintf(out, "%s : ce fichier n'est pas encore au schéma %d que ce binaire écrit — "+
		"%d migration(s) en attente, qu'« openscale config migrate » appliquerait :\n",
		path, domain.CurrentSchemaVersion, len(notes))
	for _, note := range notes {
		fmt.Fprintf(out, "  %s\n", note)
	}
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
