package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"openscale/internal/diag"
	"openscale/internal/domain"
	"openscale/internal/platform"
	"openscale/internal/printing/transport"
	"openscale/internal/store"
)

// `openscale doctor` is the subcommand of §15.1, and it WORKS WHEN NOTHING ELSE DOES.
//
// That single property shapes this file. It reads the configuration file, the database and
// the operating system directly; it never needs the HTTP layer to be up; and it refuses
// nothing. The three controls that can only be answered honestly by the running service go
// through GET /admin/api/health, and when the service is silent they say so and say how to
// make them knowable (important-11).
//
// The criterion of §18, lot L8: « openscale doctor diagnostique un service qui ne démarre
// pas et dit POURQUOI. »

// diagnosticFileName is what `--zip` writes when nothing else is asked for.
//
// It is the name §15.4 gives the archive, and the name the administration screen serves it
// under: whoever receives it by e-mail should not have to ask what it is.
const diagnosticFileName = "diagnostic.zip"

// probeTimeout bounds the HTTP client that asks the running station.
//
// It is a NETWORK deadline in the kernel's TCP stack, of the same nature as the write
// deadline of internal/web/stream.go: no business decision rests on it, and a station that
// is not listening refuses the connection at once rather than waiting for it.
const probeTimeout = 3 * time.Second

// doctorOptions is what the subcommand was told.
type doctorOptions struct {
	configPath string
	dataDir    string
	// listen overrides the address the running station is asked on. It exists for the case
	// this command is FOR: a configuration file that cannot be read carries no address, and
	// a support call still wants the three service-side controls.
	listen string
	// zip asks for diagnostic.zip beside the report.
	zip bool
	// output is where the archive goes. Empty means diagnostic.zip in the current
	// directory.
	output string
}

// runDoctor performs the controls and prints them.
//
// The exit code is exitFailure and never exitFatal: 3 is reserved by §13.4 for a station
// that cannot serve, and a diagnosis is not a station.
func runDoctor(ctx context.Context, args []string, out io.Writer) error {
	o, err := parseDoctorOptions(args, out)
	if err != nil {
		return err
	}

	clock := platform.NewSystemClock()
	doctor, err := diag.New(doctorSettings(o, clock))
	if err != nil {
		return &serviceFailure{Exit: exitFailure, Err: err,
			Message: "le diagnostic n'a pas pu être préparé : " + err.Error()}
	}

	report := doctor.Run(ctx)
	if err := report.WriteText(out); err != nil {
		return &serviceFailure{Exit: exitFailure, Err: err,
			Message: "le rapport n'a pas pu être écrit : " + err.Error()}
	}
	if o.zip {
		if err := writeArchive(ctx, doctor, o, out); err != nil {
			return err
		}
	}

	if failures := report.Count(diag.StatusFail); failures > 0 {
		// One line on stderr, because the report is already above: a script reads the exit
		// code, and a human reads the lines above and their instructions.
		return &serviceFailure{Exit: exitFailure, Message: fmt.Sprintf(
			"%d contrôle(s) en échec — les consignes sont dans le rapport ci-dessus.", failures)}
	}
	return nil
}

// doctorSettings wires everything the diagnosis is allowed to look at.
//
// Every collaborator is optional on purpose (§15.1): a base that will not open, a service
// that does not answer and a configuration that cannot be read are the three findings this
// command exists to report, not three reasons to refuse to run.
func doctorSettings(o doctorOptions, clock platform.SystemClock) diag.Options {
	scales, printers := scaleRegistry(), printerRegistry()
	return diag.Options{
		Clock:      clock,
		ConfigPath: o.configPath,
		DataDir:    o.dataDir,
		Version:    version,
		Commit:     commit,
		BuildDate:  date,
		Machine:    diag.NewMachine(clock),
		Service: diag.NewHTTPProbe(serviceAddress(o), clock,
			&http.Client{Timeout: probeTimeout}),
		// The REAL registries of this binary, so that « configuration valide » means the
		// drivers were checked and not only the shape of the file (§11.3).
		Registries: domain.Registries{
			Scales:         scales.Descriptors(),
			Printers:       printers.Descriptors(),
			Transports:     transport.Descriptors(),
			CatalogSources: catalogSourceDescriptors(),
		},
		OpenDatabase: openDatabaseForDiagnosis(clock),
		Migrations:   store.MigrationCount(),
	}
}

// serviceAddress reports where the running station is asked.
//
// The precedence is --listen, then what the configuration file says, then the address the
// neutral profile listens on. The last one is not a guess: §11.5 ships that profile, and a
// station whose configuration cannot be read is running on it right now.
func serviceAddress(o doctorOptions) string {
	if o.listen != "" {
		return o.listen
	}
	if cfg, _, _, err := readConfigLeniently(o.configPath); err == nil && cfg.Network.Listen != "" {
		return cfg.Network.Listen
	}
	return domain.NeutralProfile().Network.Listen
}

// readConfigLeniently reads the file the way the service does, migration included, so that
// `openscale config validate` judges what the station would ACTUALLY run and not what the
// file literally says.
//
// The notes are returned rather than discarded, so that `openscale config validate` can
// report them BEFORE the fault list without calling platform.LoadConfig a second time.
//
// The DECODING faults are returned for a harder reason: a block that would not decode is
// replaced by the one of the neutral profile, and that substitute passes Validate without
// a word. A caller that dropped them would answer « aucune faute » about a station that
// comes up in ERR-CFG-01 -- which is exactly what `openscale config validate` did until
// 02/08/2026, while serve, reading the same file, reported them.
func readConfigLeniently(path string) (domain.Config, []domain.MigrationNote, []domain.Fault, error) {
	return platform.LoadConfig(path)
}

// writeArchive writes diagnostic.zip where `--zip` asked for it.
func writeArchive(ctx context.Context, doctor *diag.Doctor, o doctorOptions, out io.Writer) error {
	path := o.output
	if path == "" {
		path = diagnosticFileName
	}

	// The base is opened for the archive and closed with it. It is a SECOND handle from the
	// one the controls took, and deliberately: the controls give theirs back at the end of
	// their run, and holding one open across two phases would leave the service waiting on a
	// diagnosis.
	base, journal := openJournalForDiagnosis(platform.NewSystemClock(), o.dataDir)
	if base != nil {
		defer func() { _ = base.Close() }()
	}
	bundle, err := diag.NewBundle(doctor, journal, labelsDir(o.dataDir))
	if err != nil {
		return &serviceFailure{Exit: exitFailure, Err: err,
			Message: "l'archive de diagnostic n'a pas pu être préparée : " + err.Error()}
	}

	file, err := os.Create(path)
	if err != nil {
		return &serviceFailure{Exit: exitFailure, Err: err, Message: fmt.Sprintf(
			"le fichier %s ne peut pas être écrit : %v", path, err)}
	}
	if err := bundle.Diagnostic(ctx, file); err != nil {
		_ = file.Close()
		return &serviceFailure{Exit: exitFailure, Err: err, Message: fmt.Sprintf(
			"l'archive %s est incomplète : %v", path, err)}
	}
	if err := file.Close(); err != nil {
		return &serviceFailure{Exit: exitFailure, Err: err, Message: fmt.Sprintf(
			"l'archive %s n'a pas pu être refermée : %v", path, err)}
	}

	fmt.Fprintf(out, "\nFichier de diagnostic écrit dans %s.\n", path)
	fmt.Fprintf(out, "Il ne contient aucun mot de passe et aucune adresse privée : "+
		"vous pouvez l'envoyer sans le relire.\n")
	return nil
}

// parseDoctorOptions resolves the flags against the environment and the defaults of §11.1.
func parseDoctorOptions(args []string, out io.Writer) (doctorOptions, error) {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(out)
	var (
		configPath = fs.String("config", os.Getenv("OPENSCALE_CONFIG"), "fichier de configuration")
		dataDir    = fs.String("data", os.Getenv("OPENSCALE_DATA"), "répertoire de données")
		listen     = fs.String("listen", "", "adresse du service à interroger")
		zip        = fs.Bool("zip", false, "écrire aussi diagnostic.zip")
		output     = fs.String("output", "", "emplacement de l'archive")
	)
	fs.Usage = func() {
		fmt.Fprint(out, `Usage : openscale doctor [--zip] [--output f.zip] [--config fichier] [--data répertoire]

Les contrôles de §15.4. Chacun dit ce qui a été vérifié, comment cela s'est
passé, et ce qu'il faut FAIRE si c'est rouge. La commande fonctionne même quand le
service ne démarre pas — c'est précisément à cela qu'elle sert.

Trois contrôles interrogent le service en cours, parce que lui seul peut répondre :
la file d'impression telle qu'il la voit, la cadence qu'il a mesurée, et la source du
catalogue avec SES droits. Quand il ne répond pas, ils le disent.

Options :
  --zip                    écrire aussi le fichier de diagnostic à envoyer au support
  --output <fichier>        où écrire l'archive ; sinon diagnostic.zip ici
  --config <fichier>        configuration du poste ; sinon OPENSCALE_CONFIG, sinon
                            l'emplacement par défaut du système
  --data <répertoire>       base, images et étiquettes ; sinon OPENSCALE_DATA, sinon
                            l'emplacement par défaut du système
  --listen <hôte:port>      adresse du service ; sinon celle du fichier de configuration

Code de retour : 0 quand aucun contrôle n'est en échec, 1 sinon.
`)
	}
	positional, err := parseMixed(fs, args)
	if err != nil {
		return doctorOptions{}, err
	}
	if len(positional) != 0 {
		fs.Usage()
		return doctorOptions{}, fmt.Errorf("argument inattendu %q : doctor ne prend que des options",
			positional[0])
	}

	o := doctorOptions{configPath: *configPath, dataDir: *dataDir, listen: *listen,
		zip: *zip, output: *output}
	if o.output != "" {
		// --output without --zip is an instruction nobody could carry out silently: the
		// intention is obvious, so it is honoured rather than refused.
		o.zip = true
	}
	if o.configPath == "" {
		o.configPath = platform.DefaultConfigPath()
	}
	if o.dataDir == "" {
		o.dataDir = platform.DefaultDataDir()
	}
	return o, nil
}

// --- The archive of a RUNNING station ---------------------------------------

// newStationDiagnostic wires GET /admin/api/diagnostic.zip over a station that is up.
//
// Two things differ from the command line, and both matter.
//
// The database is BORROWED, never opened a second time: the station owns it, and a
// diagnosis that closed it would take the station down with it. borrowedDatabase is what
// carries that distinction across the seam.
//
// The service is asked over HTTP, on its own listening address. That is deliberate rather
// than a shortcut: health.json then carries the route of §14.5 VERBATIM, byte for byte what
// the administration screen reads, so a support call is looking at the same document the
// volunteer was looking at. Reading the snapshot directly would produce a second rendering
// of the same facts, and the day the two disagree nobody would know which one to believe.
func newStationDiagnostic(o serveOptions, clock platform.SystemClock, address string,
	registries domain.Registries, db *store.DB) (*diag.Bundle, error) {
	doctor, err := diag.New(diag.Options{
		Clock:      clock,
		ConfigPath: o.configPath,
		DataDir:    o.dataDir,
		Version:    version,
		Commit:     commit,
		BuildDate:  date,
		Machine:    diag.NewMachine(clock),
		Service:    diag.NewHTTPProbe(address, clock, &http.Client{Timeout: probeTimeout}),
		Registries: registries,
		OpenDatabase: func(string) (diag.Database, error) {
			return borrowedDatabase{db}, nil
		},
		Migrations: store.MigrationCount(),
	})
	if err != nil {
		return nil, err
	}
	return diag.NewBundle(doctor, diagJournal{db}, labelsDir(o.dataDir))
}

// borrowedDatabase is the station's own base, handed to a diagnosis that must not close it.
//
// The doctor gives back what it opened at the end of every run, which is right when IT
// opened the file — and catastrophic when the file is the one the station is weighing with.
// Close therefore does nothing, and says why.
type borrowedDatabase struct{ db *store.DB }

// Path reports the file the base lives in.
func (b borrowedDatabase) Path() string { return b.db.Path() }

// SchemaVersion reports how many migrations have been applied.
func (b borrowedDatabase) SchemaVersion() (int, error) { return b.db.SchemaVersion() }

// IntegrityCheck runs the integrity check on the live base.
//
// It is measured under 300 ms on 25 000 rows (§12.5) and it holds no write lock, so a
// customer weighing at that instant waits for nothing.
func (b borrowedDatabase) IntegrityCheck(ctx context.Context) error {
	return b.db.IntegrityCheck(ctx)
}

// Close does NOTHING: the station owns this base and Station.Stop closes it last, after the
// workers have been drained (§13.4).
func (b borrowedDatabase) Close() error { return nil }

// --- The adapters -----------------------------------------------------------

// openDatabaseForDiagnosis builds the opener the two database controls use.
//
// It CLASSIFIES the refusal, and that is the whole reason it exists: ERR-DB-01 repairs or
// restores a file, ERR-DB-02 updates a binary, and internal/diag cannot tell them apart
// because it imports no storage package (§5.2, cut 3).
func openDatabaseForDiagnosis(clock platform.SystemClock) func(string) (diag.Database, error) {
	return func(path string) (diag.Database, error) {
		db, err := store.Open(path, clock)
		if err == nil {
			return db, nil
		}
		if errors.Is(err, store.ErrSchemaFromNewerVersion) {
			return nil, &diag.DatabaseFailure{Code: "ERR-DB-02", Err: err,
				Message: "la base a été créée par une version plus récente : " + err.Error()}
		}
		return nil, &diag.DatabaseFailure{Code: "ERR-DB-01", Err: err,
			Message: "la base ne peut pas être ouverte : " + err.Error()}
	}
}

// openJournalForDiagnosis opens the base the archive reads, or reports nothing.
//
// A nil journal is a valid answer and NOT an error: diagnostic.zip is worth having when the
// base is corrupt, and the archive records why the journal members are missing instead of
// refusing to be built.
func openJournalForDiagnosis(clock platform.SystemClock, dataDir string) (io.Closer, diag.Journal) {
	if dataDir == "" {
		return nil, nil
	}
	db, err := store.Open(platform.DatabasePath(dataDir), clock)
	if err != nil {
		return nil, nil
	}
	return db, diagJournal{db}
}

// diagJournal adapts the station base to what diagnostic.zip reads.
//
// Two structures carrying the same values, and the conversion is the price of cut 3:
// internal/diag names no storage type, so it declares what it needs and the composition root
// joins the two — exactly as internal/web does.
type diagJournal struct{ db *store.DB }

// Weighings returns the most recent weighings, newest first.
func (j diagJournal) Weighings(ctx context.Context, limit int) ([]domain.Weighing, error) {
	return j.db.Weighings(ctx, store.JournalFilter{Limit: limit})
}

// TechnicalEntries returns the most recent technical lines, newest first.
func (j diagJournal) TechnicalEntries(ctx context.Context, limit int) ([]diag.TechnicalEntry, error) {
	lines, err := j.db.TechnicalEntries(ctx, store.TechnicalFilter{Limit: limit})
	if err != nil {
		return nil, err
	}
	out := make([]diag.TechnicalEntry, 0, len(lines))
	for _, line := range lines {
		out = append(out, diag.TechnicalEntry{
			OccurredAt: line.OccurredAt, Level: line.Level, Source: line.Source,
			Code: line.Code, Message: line.Message, Detail: line.Detail,
		})
	}
	return out, nil
}

// Imports returns the import history, most recent first.
func (j diagJournal) Imports(ctx context.Context, limit int) ([]domain.Import, error) {
	return j.db.Imports(ctx, limit, 0)
}

// CatalogCounts reports what the catalog IN SERVICE holds.
//
// It is counted from the base and not from the running station, so that the figure is in the
// archive even when the service is down — which is when somebody presses the button. Withdrawn
// products are counted apart: a product absent from the last file is marked withdrawn at a
// date, never deleted (§10.9), and counting it as a product in service would inflate the grid.
func (j diagJournal) CatalogCounts(ctx context.Context) (diag.CatalogCounts, error) {
	rows, err := j.db.AllProducts(ctx)
	if err != nil {
		return diag.CatalogCounts{}, err
	}
	out := diag.CatalogCounts{ByCategory: make(map[string]int)}
	for _, row := range rows {
		if !row.WithdrawnAt.IsZero() {
			out.Withdrawn++
			continue
		}
		out.Products++
		if row.Product.Qualification == domain.Weighable {
			out.Weighable++
			out.ByCategory[row.Product.CategoryCode]++
		}
	}
	return out, nil
}
