package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"openscale/internal/domain"
	"openscale/internal/platform"
	"openscale/internal/printing/transport"
	"openscale/internal/web"
)

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

	cfg, notes, err := readConfigLeniently(path)
	if err != nil {
		return fmt.Errorf("le fichier de configuration %s ne peut pas être lu : %w", path, err)
	}

	switch action {
	case "validate":
		return validateConfig(out, path, cfg, notes)
	case "export":
		return exportConfig(out, cfg, *hardware, *output)
	case "fingerprint":
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

// validateConfig runs the controls of §11.3 with the REAL registries of this binary,
// and prints every fault at once.
//
// Every fault and not the first: a volunteer who came to fix one file should leave
// having fixed it, and not discover the second fault after a restart. The exit code is
// what makes it usable from install.ps1 — a non-zero status means « this station will
// start in factory configuration ».
func validateConfig(out io.Writer, path string, cfg domain.Config, notes []domain.MigrationNote) error {
	reportPendingMigrations(out, path, notes)

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

// minPasswordLength is the floor POST /admin/api/session/recovery already holds (§14.4).
//
// The same figure in the two places that set a password, because a station where the
// terminal accepted four characters and the screen refused them would be a station whose
// rule depends on which door somebody came through.
const minPasswordLength = 8

// setAdminPassword is `openscale config password` (§14.4).
//
// It writes the FILE, through the same store the administration screen saves with, so a
// password set from a terminal rotates the versions and lands atomically like any other
// change. The station does not see it before it restarts: nothing re-reads config.json
// while the service runs, and pretending otherwise would have somebody typing a password
// that works only after the next power cut.
func setAdminPassword(in io.Reader, out io.Writer, path string) error {
	store, err := platform.NewConfigStore(path)
	if err != nil {
		return err
	}
	ctx := context.Background()
	cfg, err := store.Read(ctx)
	if err != nil {
		return fmt.Errorf("le fichier de configuration %s ne peut pas être lu : %w", path, err)
	}

	fmt.Fprintf(out, "Nouveau mot de passe d'administration pour %s\n"+
		"(au moins %d caractères, il s'affiche à l'écran) : ", path, minPasswordLength)
	typed, err := readSecretLine(in)
	if err != nil {
		return err
	}
	if len([]rune(typed)) < minPasswordLength {
		return fmt.Errorf("le mot de passe doit faire au moins %d caractères", minPasswordLength)
	}

	hash, err := web.HashSecret(typed)
	if err != nil {
		return err
	}
	cfg.Admin.PasswordHash = hash
	cfg.ModifiedAt = platform.NewSystemClock().Now()
	if err := store.Save(ctx, cfg); err != nil {
		return err
	}

	fmt.Fprintf(out, "\nMot de passe d'administration posé dans %s.\n", path)
	if cfg.Admin.RecoveryCodeHash == "" {
		fmt.Fprintf(out, "Ce poste n'a AUCUN code de secours : sans lui, ce mot de passe "+
			"perdu se rattrape uniquement ici. Tirez-en un avec « openscale config "+
			"recovery-code » et recopiez-le sur la fiche d'installation.\n")
	}
	fmt.Fprintf(out, "Redémarrez le service pour que le poste le prenne en compte.\n")
	return nil
}

// mintRecoveryCode is `openscale config recovery-code` (§14.4, important-10).
//
// The code is shown ONCE, in clear, and never again: the configuration keeps its argon2id
// hash and nothing else. Whoever runs this command has one job left, and it is written on
// the last line — copy the eight characters onto the installation sheet, which goes into
// the shop's folder and not onto the station.
func mintRecoveryCode(out io.Writer, path string) error {
	store, err := platform.NewConfigStore(path)
	if err != nil {
		return err
	}
	ctx := context.Background()
	cfg, err := store.Read(ctx)
	if err != nil {
		return fmt.Errorf("le fichier de configuration %s ne peut pas être lu : %w", path, err)
	}
	replacing := cfg.Admin.RecoveryCodeHash != ""

	code, err := web.NewRecoveryCode()
	if err != nil {
		return err
	}
	hash, err := web.HashSecret(code)
	if err != nil {
		return err
	}
	cfg.Admin.RecoveryCodeHash = hash
	cfg.ModifiedAt = platform.NewSystemClock().Now()
	if err := store.Save(ctx, cfg); err != nil {
		return err
	}

	if replacing {
		fmt.Fprintf(out, "L'ancien code de secours de ce poste ne fonctionne plus : "+
			"la fiche déjà classée est à corriger.\n")
	}
	fmt.Fprintf(out, "Code de secours de ce poste : %s\n", code)
	fmt.Fprintf(out, "Recopiez-le sur la fiche d'installation MAINTENANT : il ne sera "+
		"plus jamais affiché.\n")
	return nil
}

// readSecretLine reads ONE line off the standard input.
//
// No echo suppression, and it is a deliberate refusal rather than an oversight: turning
// the terminal echo off means a terminal package, which means a seventh module in a
// perimeter §17.1 closes at six, bought for the one call it would take — ADR-039 weighs
// a dependency on the surface actually called, and this one is a single call. The usage
// text says the line is visible, and the machine it is typed on is the station's own
// console.
func readSecretLine(in io.Reader) (string, error) {
	if in == nil {
		return "", errors.New("aucune entrée standard : le mot de passe se tape au clavier, " +
			"ou s'envoie par un tube")
	}
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("lecture du mot de passe : %w", err)
	}
	// Typed on a Windows console the line ends with \r\n, piped from a file it may end
	// with nothing at all. Only those two characters go: a password is allowed to end
	// with a space, and trimming it would refuse tomorrow what it accepted today.
	return strings.TrimRight(line, "\r\n"), nil
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

// migrateConfig is `openscale config migrate`.
//
// It writes through the same store the administration screen saves with, so a migration
// rotates config.json.1 … .5 and lands atomically like any other change. Nothing new is
// invented for it, and that is the point: the version of before is one file away.
//
// It is IDEMPOTENT. update.ps1 and update.sh call it at every update, and a station that is
// already at this schema must come out of it with its file untouched -- rotating five
// versions over a no-operation is how the version that mattered falls off the end.
//
// A refused point suspends the WHOLE write, not only its own key: what could be carried
// stays computed, correctly, in the cfg this run holds in memory, but nothing at all
// reaches disk while a single point is still refused -- see the comment on the refusal
// branch below for why that has to hold even for a file that carries one point migrate CAN
// write and one it cannot.
func migrateConfig(out io.Writer, path string) error {
	cfg, notes, _, err := platform.LoadConfig(path)
	if err != nil {
		return fmt.Errorf("le fichier de configuration %s ne peut pas être lu : %w", path, err)
	}

	// A key control 20 refuses outright earns NO note of its own when Migrate has no
	// translation to attempt for it: the six keys of the numbering plan are a pure
	// refusal, unchanged since they entered the code already retired (configmigration.go),
	// and migrationSteps never touches them. cfg.Retired() is the only place that survives
	// for them, and it is also what ConfigStore.Save is about to consult -- so a key still
	// there is folded into the very same accounting as the notes, under the same name a
	// note would use, before Save is ever called.
	retired := cfg.Retired()
	named := make(map[string]bool, len(notes))
	for _, note := range notes {
		named[note.Key] = true
	}
	for _, key := range retired {
		if named[key] {
			continue
		}
		notes = append(notes, domain.MigrationNote{
			Key: key, Action: domain.MigrationRefused, Message: domain.RetiredKeyReason(key),
		})
	}

	if len(notes) == 0 {
		fmt.Fprintf(out, "%s est déjà à la forme que ce binaire lit : rien à faire.\n", path)
		return nil
	}

	fmt.Fprintf(out, "%s : %d changement(s).\n", path, len(notes))
	refused := 0
	for _, note := range notes {
		fmt.Fprintf(out, "  %s\n", note)
		if note.Action == domain.MigrationRefused {
			refused++
		}
	}

	// ConfigStore.Save calls cfg.RefuseIfRetired, and a MIXED file -- one point carried,
	// one refused -- would reach it and be refused there too, only AFTER this command had
	// already said it was writing. Leaving here, before Save is ever called, is what keeps
	// « rien n'est écrit » true unconditionally, whether the refusal came with a note (an
	// unconvertible discount, a file from a newer binary) or without one (the numbering
	// plan, folded in just above).
	if refused > 0 {
		return &serviceFailure{Exit: exitFailure, Message: fmt.Sprintf(
			"%s comporte %d point(s) que ce binaire ne devine pas : le fichier n'est pas "+
				"modifié, tranchez-le puis relancez la migration", path, refused)}
	}

	store, err := platform.NewConfigStore(path)
	if err != nil {
		return err
	}
	cfg.ModifiedAt = platform.NewSystemClock().Now()
	if err := store.Save(context.Background(), cfg); err != nil {
		return fmt.Errorf("%s n'a pas pu être réécrit : %w", path, err)
	}
	fmt.Fprintf(out, "%s réécrit ; la version d'avant est dans %s.1.\n", path, path)
	fmt.Fprintf(out, "Redémarrez le service pour qu'il lise le fichier réécrit.\n")
	return nil
}
