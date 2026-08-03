package diag

import (
	"context"
	"errors"
	"fmt"

	"openscale/internal/domain"
	"openscale/internal/platform"
	"openscale/internal/station/ports"
)

// This file is the doctor itself and nothing else: what it is given, what it opens and
// reads ONCE, the order it puts the controls in, and the handful of things every family of
// controls uses. The controls live beside it, one file per family — doctor_service.go,
// doctor_storage.go, doctor_devices.go, doctor_config.go, doctor_system.go — and Run below
// is the only place their order is declared.

// Database is the station base as the two database controls read it.
//
// Declared HERE, on the consumer's side: *store.DB satisfies it as it stands, and a test
// drives the two controls with a double that opens no file (§5.2, cut 3 — internal/diag
// names no storage package).
type Database interface {
	// Path reports the file the base lives in, which is what the report names.
	Path() string
	// SchemaVersion reports how many migrations have been applied to this file.
	SchemaVersion() (int, error)
	// IntegrityCheck runs the integrity check and reports the first problem found.
	IntegrityCheck(ctx context.Context) error
	// Close releases the handles this doctor took. The doctor owns them for the length
	// of one run and never longer: the SERVICE owns the base the rest of the time.
	Close() error
}

// DatabaseFailure is a refusal to open the base that names its ERR-DB code.
//
// The two failures have two different remedies — ERR-DB-01 repairs or restores a FILE,
// ERR-DB-02 updates a BINARY — so the doctor has to tell them apart. It cannot do so by
// inspecting a sentinel of internal/store, which it does not import, so the composition
// root classifies and this type carries the answer across the seam.
type DatabaseFailure struct {
	// Code is "ERR-DB-01" or "ERR-DB-02".
	Code string
	// Message is FRENCH and complete.
	Message string
	Err     error
}

// Error reports the French sentence, which is what the control shows.
func (f *DatabaseFailure) Error() string { return f.Message }

// Unwrap yields the failure this one was built on.
func (f *DatabaseFailure) Unwrap() error { return f.Err }

// Options is everything `openscale doctor` is given.
//
// Only Clock is required. EVERY other collaborator may be absent, and its absence is
// answered honestly rather than hidden: a station whose service will not start is the one
// case this command exists for, so a doctor that refused to run without a database or
// without a service would refuse exactly when it is needed (§15.1).
type Options struct {
	Clock ports.Clock

	// ConfigPath and DataDir are what `serve` would have used, resolved by the caller
	// against --config, --data, the two environment variables of §11.1 and the defaults.
	ConfigPath string
	DataDir    string

	Version   string
	Commit    string
	BuildDate string

	// Machine is the host. Nil answers « rien n'a pu être demandé au système ».
	Machine Machine
	// Service is the running station. Nil answers the same way, and the three controls
	// that need it say how to make them knowable.
	Service ServiceProbe
	// Registries is what a configuration is validated against (§11.3): the drivers and
	// templates THIS binary carries. An empty one would report a valid configuration as
	// invalid, so the configuration control says so rather than accusing the file.
	Registries domain.Registries

	// OpenDatabase opens the station base. The error it returns should be, or should
	// wrap, a *DatabaseFailure; a plain error is treated as ERR-DB-01.
	OpenDatabase func(path string) (Database, error)
	// Migrations is how many migration scripts this binary carries, which is the number
	// the applied schema version is compared against. Zero means « the caller did not
	// say », and the control reports that instead of guessing.
	Migrations int
}

// Doctor performs the controls of §15.4.
type Doctor struct {
	o Options
}

// New builds the doctor. It refuses only what it cannot work without.
func New(o Options) (*Doctor, error) {
	if o.Clock == nil {
		return nil, errors.New("diag.New: pas d'horloge ; tout instant se lit sur l'horloge INJECTÉE (§5.3)")
	}
	if o.Machine == nil {
		o.Machine = silentMachine{}
	}
	return &Doctor{o: o}, nil
}

// loadedConfig is the configuration file as the run read it, once.
//
// Six controls need it and one control judges it: reading it once means the report cannot
// contradict itself by reading a file somebody edits between two controls.
type loadedConfig struct {
	// Present is false when the file could not be read at all, which is NOT the
	// « configuration invalide » of §11.3 and does not have the same remedy.
	Present bool
	// Parsed is false when the bytes are not usable JSON.
	Parsed bool
	Config domain.Config
	Faults []domain.Fault
	// DecodeFaults are the ones DecodeConfigBlockByBlock produced. They are also in Faults,
	// and they are kept apart because they carry a consequence no Validate fault carries: a
	// decoding fault means a BLOCK WAS REPLACED by the neutral profile, so anything computed
	// over the configuration AS A WHOLE -- the fingerprint above all -- then describes a
	// document nobody wrote. A file whose every block decoded describes itself perfectly
	// well even when every value in it is wrong.
	DecodeFaults []domain.Fault
	// MigrationNotes is what platform.LoadConfig had to change to bring the document up
	// to the schema this binary speaks. It is the ONLY trace left of that once Config is
	// built: LoadConfig migrates the document BEFORE decoding it, so Config.Version reads
	// domain.CurrentSchemaVersion whether the file on disk was one version behind or
	// already current — comparing it against anything would compare a number against
	// itself. An empty slice means the file needed nothing.
	MigrationNotes []domain.MigrationNote
	Err            error
}

// Run performs the controls, in the order §15.4 enumerates them, and hands back
// the report.
//
// It never returns an error: a diagnosis that refuses to produce a report because one of
// its own steps failed is a diagnosis nobody can read. Everything that went wrong is IN
// the report, which is the point.
func (d *Doctor) Run(ctx context.Context) Report {
	loaded := d.readConfiguration()
	health, healthErr := d.askTheService(ctx)
	base, baseErr := d.openBase()
	if base != nil {
		defer func() { _ = base.Close() }()
	}

	report := Report{
		At:          d.o.Clock.Now(),
		Version:     d.o.Version,
		Commit:      d.o.Commit,
		BuildDate:   d.o.BuildDate,
		System:      d.o.Machine.Describe(ctx),
		ConfigPath:  d.o.ConfigPath,
		DataDir:     d.o.DataDir,
		Station:     loaded.Config.Station.Number,
		StationName: loaded.Config.Station.Name,
		Coop:        loaded.Config.Station.Coop,
	}
	// No fingerprint when a block was substituted, which is the rule ConfigStore.Versions
	// already holds on the other document support reads (§14.4): « elle est inconnue, pas
	// inventée ». The eight characters would otherwise describe a configuration nobody
	// declared, in the header of the very file four stations get compared with -- and right
	// above a red control saying so. Nothing is refused by leaving it out: the report is
	// still produced, and the configuration control still names the block.
	if loaded.Parsed && len(loaded.DecodeFaults) == 0 {
		report.Fingerprint = loaded.Config.Fingerprint()
	}

	report.Controls = []Control{
		d.checkService(ctx),
		d.checkKioskTask(ctx),
		d.checkUnattendedRestart(ctx),
		d.checkDataDirectory(),
		d.checkDiskSpace(loaded),
		d.checkListenAddress(ctx, loaded),
		d.checkConfiguration(loaded),
		d.checkDatabase(ctx, base, baseErr),
		d.checkMigrations(base, baseErr),
		d.checkSerialPort(ctx, loaded),
		d.checkPrintQueue(ctx, loaded, health, healthErr),
		d.checkScaleRate(loaded, health, healthErr),
		d.checkCatalogSource(loaded, health, healthErr),
		d.checkSystemClock(loaded),
		d.checkPowerSettings(ctx),
		d.checkRebootPermission(ctx),
		d.checkNavigationLock(ctx),
	}
	for i := range report.Controls {
		report.Controls[i].Rank = i + 1
	}
	return report
}

// Health hands back what the running station answered, for the archive of §15.4.
//
// It is the SAME call the three service-dependent controls make, exposed so that
// diagnostic.zip does not ask a second time and cannot therefore ship a report and a
// health payload that disagree.
func (d *Doctor) Health(ctx context.Context) (Health, error) { return d.askTheService(ctx) }

// askTheService reads GET /admin/api/health once.
func (d *Doctor) askTheService(ctx context.Context) (Health, error) {
	if d.o.Service == nil {
		return Health{}, fmt.Errorf("%w : aucune adresse de service n'a été fournie à cette commande",
			ErrServiceSilent)
	}
	return d.o.Service.Health(ctx)
}

// liveness asks /healthz, and only to tell « held by us » from « held by something else ».
func (d *Doctor) liveness(ctx context.Context) (Liveness, error) {
	if d.o.Service == nil {
		return Liveness{}, ErrServiceSilent
	}
	return d.o.Service.Liveness(ctx)
}

// openBase opens the station base for the length of the run.
func (d *Doctor) openBase() (Database, error) {
	if d.o.OpenDatabase == nil {
		return nil, errors.New("aucun ouvreur de base n'a été fourni à cette commande")
	}
	if d.o.DataDir == "" {
		return nil, errors.New("aucun répertoire de données n'a été fourni à cette commande")
	}
	return d.o.OpenDatabase(platform.DatabasePath(d.o.DataDir))
}

// readConfiguration reads config.json once, through the same door serve and `openscale
// config` read it with (platform.LoadConfig), and tells « unreadable » from « invalid ».
//
// The distinction decides a remedy and it is the one §11.3 insists on: an INVALID
// configuration is one we understood and can list the faults of, field by field; a file
// that does not parse yields nothing an administration screen could safely write back.
//
// Parsed is derived from domain.WholeDocumentField and not from a second parse of the
// file: DecodeConfigBlockByBlock already tells « the document itself did not decode » from
// « one block did not », by the Field its fault names, and re-deriving the same verdict a
// second way here is how the two come to disagree.
func (d *Doctor) readConfiguration() loadedConfig {
	if d.o.ConfigPath == "" {
		return loadedConfig{Err: errors.New("aucun fichier de configuration n'a été désigné")}
	}
	cfg, notes, decodeFaults, err := platform.LoadConfig(d.o.ConfigPath)
	if err != nil {
		return loadedConfig{Err: err}
	}
	out := loadedConfig{Present: true, Config: cfg, MigrationNotes: notes}
	if wholeDocument := wholeDocumentFault(decodeFaults); wholeDocument != nil {
		out.Err = errors.New(wholeDocument.Message)
		return out
	}
	out.Parsed = true
	out.DecodeFaults = decodeFaults
	// A fresh slice rather than appending onto decodeFaults: the two fields would otherwise
	// share a backing array, and a future append on one would be reading the other.
	out.Faults = append(append([]domain.Fault{}, decodeFaults...),
		out.Config.Validate(d.o.Registries)...)
	return out
}

// wholeDocumentFault reports the fault that names the DOCUMENT itself, or nil when every
// fault names a block within it.
func wholeDocumentFault(faults []domain.Fault) *domain.Fault {
	for i := range faults {
		if faults[i].Field == domain.WholeDocumentField {
			return &faults[i]
		}
	}
	return nil
}

// --- Shared -----------------------------------------------------------------

// clockLayout is how this report spells an instant: local time, seconds, and the offset.
//
// The offset is what makes an archive readable six months later, from another timezone,
// by somebody reconciling a weighing journal against a till.
const clockLayout = "2006-01-02 15:04:05 -07:00"

// detailSuffix appends the technical tail of an error, or nothing.
func detailSuffix(err error) string {
	if err == nil {
		return ""
	}
	return " : " + err.Error()
}
