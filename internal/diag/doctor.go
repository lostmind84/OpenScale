package diag

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"openscale/internal/domain"
	"openscale/internal/platform"
	"openscale/internal/station/ports"
)

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

// --- 1. The service ---------------------------------------------------------

func (d *Doctor) checkService(ctx context.Context) Control {
	control := Control{ID: ControlService, Checked: "Service OpenScale présent et démarré"}
	state, err := d.o.Machine.Service(ctx)
	switch {
	case err != nil:
		control.Status, control.Observed = StatusUnknown, "le gestionnaire de services n'a pas répondu : "+err.Error()
		control.Remedy = "Relancez cette commande depuis une invite ADMINISTRATEUR : l'état d'un " +
			"service n'est pas lisible par tout le monde."
	case !state.Determined:
		control.Status, control.Observed = StatusUnknown, "ce système n'expose pas de gestionnaire de services interrogeable"
		control.Remedy = "Vérifiez à la main que le poste est lancé, puis ouvrez http://" +
			"127.0.0.1:8080/ sur l'écran : si la page s'affiche, le service tourne."
	case !state.Known:
		control.Status = StatusFail
		control.Observed = fmt.Sprintf("aucun service « %s » n'est déclaré sur ce poste", state.Name)
		control.Remedy = serviceInstallRemedy()
	case !state.Running:
		control.Status = StatusFail
		control.Observed = fmt.Sprintf("le service « %s » est installé mais arrêté (%s)", state.Name, state.Detail)
		// THIS is the sentence the L8 criterion asks for: doctor diagnoses a service that
		// will not start AND SAYS WHY — by naming the four controls that carry the reason.
		control.Remedy = serviceStartRemedy()
	case !state.Automatic:
		control.Status = StatusWarn
		control.Observed = fmt.Sprintf("le service « %s » tourne, et son démarrage n'est pas automatique (%s)",
			state.Name, state.Detail)
		control.Remedy = "Après une coupure de courant, ce poste ne redémarrera pas seul. Passez-le " +
			"en démarrage automatique : sc config OpenScale start= auto\n" +
			"Si ce poste est le poste pilote, c'est voulu (§18, lot L9) et il n'y a rien à faire."
	default:
		control.Status = StatusPass
		control.Observed = fmt.Sprintf("le service « %s » tourne, démarrage automatique (%s)",
			state.Name, state.Detail)
	}
	return control
}

// serviceInstallRemedy is the instruction for a service the manager has never heard of.
func serviceInstallRemedy() string {
	if runtime.GOOS == "windows" {
		return "Relancez install.ps1 en administrateur (§15.2), ou installez le service à la main :\n" +
			`"C:\Program Files\OpenScale\openscale.exe" service install` + "\n" +
			"puis sc config OpenScale start= auto"
	}
	return "Installez l'unité : systemctl enable --now openscale.service (§15.3)."
}

// serviceStartRemedy names WHERE the reason for a failed start is written.
func serviceStartRemedy() string {
	command := "systemctl start openscale.service"
	logs := "journalctl -u openscale.service -n 50"
	if runtime.GOOS == "windows" {
		command = "sc start OpenScale"
		logs = `le fichier C:\ProgramData\OpenScale\data\logs\openscale.log`
	}
	return "Démarrez-le : " + command + "\nS'il s'arrête aussitôt, la raison est dans l'un des " +
		"contrôles 6, 7, 8 ou 10 ci-dessous — adresse d'écoute déjà prise, configuration " +
		"illisible, base inutilisable, port série absent — et le détail est dans " + logs + "."
}

// --- 2. The kiosk task ------------------------------------------------------

func (d *Doctor) checkKioskTask(ctx context.Context) Control {
	control := Control{ID: ControlKioskTask, Checked: "Tâche du kiosque présente"}
	state, err := d.o.Machine.KioskTask(ctx)
	switch {
	case err != nil:
		control.Status, control.Observed = StatusUnknown, "la tâche du kiosque n'a pas pu être interrogée : "+err.Error()
		control.Remedy = "Relancez cette commande depuis une invite ADMINISTRATEUR : le dossier " +
			"des tâches planifiées n'est pas lisible par tout le monde, et « je n'ai pas pu " +
			"regarder » n'est pas « la tâche est absente ».\n" +
			"Tant que ce contrôle est INCONNU, ne réinstallez rien."
	case !state.Determined:
		control.Status, control.Observed = StatusUnknown, "ce système n'expose pas de planificateur interrogeable"
		control.Remedy = "Vérifiez à la main qu'un navigateur en plein écran s'ouvre à l'ouverture de session."
	case !state.Known:
		control.Status = StatusFail
		control.Observed = fmt.Sprintf("aucune tâche « %s » n'est déclarée", state.Name)
		control.Remedy = kioskInstallRemedy()
	default:
		control.Status = StatusPass
		control.Observed = fmt.Sprintf("la tâche « %s » est déclarée (%s)", state.Name, or(state.Detail, "état non lu"))
	}
	return control
}

// kioskInstallRemedy is the instruction for a missing kiosk task.
//
// It says what the ABSENCE costs, because a volunteer reading « tâche absente » has no
// way of knowing that the service can be perfectly healthy while the screen stays black.
func kioskInstallRemedy() string {
	if runtime.GOOS == "windows" {
		return "Sans elle, le service tourne mais l'écran client ne s'ouvre jamais. Relancez " +
			"install.ps1 en administrateur (§15.2), ou recréez la tâche :\n" +
			`schtasks /create /tn "OpenScale-Kiosk" /xml openscale-kiosk.xml /f`
	}
	return "Sans elle, le service tourne mais l'écran client ne s'ouvre jamais. Activez l'unité " +
		"du kiosque : systemctl enable --now openscale-kiosk.service (§15.3)."
}

// --- 3. Unattended restart --------------------------------------------------

// codeUnattendedRestart is ERR-SYS-08, and §14.4 allocates it to exactly this fact.
const codeUnattendedRestart = "ERR-SYS-08"

func (d *Doctor) checkUnattendedRestart(ctx context.Context) Control {
	return UnattendedRestartControl(ctx, d.o.Machine)
}

// UnattendedRestartControl is control 3, and it is EXPORTED because §14.4 puts the same
// verdict on the administration dashboard (bloquant-7).
//
// One function for the two readers. A volunteer reading « redémarrage sans intervention :
// NON CONFIGURÉ » on the screen and whoever reads `doctor.txt` an hour later are looking
// at the same registry key, and two implementations of the same three conditions would
// eventually tell them two different things about it.
func UnattendedRestartControl(ctx context.Context, machine Machine) Control {
	control := Control{ID: ControlUnattendedRestart,
		Checked: "Redémarrage sans intervention configuré (OUI / NON)"}
	state, err := machine.AutoLogon(ctx)
	switch {
	case err != nil:
		control.Status, control.Observed = StatusUnknown, "la configuration du redémarrage n'a pas pu être lue : "+err.Error()
		control.Remedy = "Relancez cette commande depuis une invite administrateur."
	case !state.Determined:
		control.Status, control.Observed = StatusUnknown, "ce système ne dit pas si la session s'ouvre seule"
		control.Remedy = "Faites la recette de §15.5 : redémarrez la machine et vérifiez que le poste " +
			"revient SEUL sur l'écran client, sans que personne tape de mot de passe."
	case !state.Enabled:
		control.Status, control.Code = StatusFail, codeUnattendedRestart
		control.Observed = "NON : après une coupure de courant, ce poste restera sur l'écran de " +
			"connexion et personne dans l'équipe du samedi n'a le mot de passe. " + state.Detail
		control.Remedy = unattendedRestartRemedy()
	case state.Expected != "" && !strings.EqualFold(state.Account, state.Expected):
		control.Status, control.Code = StatusFail, codeUnattendedRestart
		control.Observed = fmt.Sprintf("la session s'ouvre seule pour le compte « %s », alors que le "+
			"kiosque tourne sous « %s » : ce n'est pas la session qui lance l'écran client",
			state.Account, state.Expected)
		control.Remedy = unattendedRestartRemedy()
	default:
		control.Status = StatusPass
		control.Observed = fmt.Sprintf("OUI, pour le compte « %s »", or(state.Account, "non nommé"))
	}
	return control
}

// unattendedRestartRemedy is the instruction of bloquant-7, recipe included.
//
// The recipe is part of the remedy and not an extra: the previous plan wrote the registry
// key and told a human to finish the job, which was done once and NEVER VERIFIED AGAIN.
func unattendedRestartRemedy() string {
	if runtime.GOOS == "windows" {
		return "Relancez install.ps1 en administrateur — c'est son étape 3 (§15.2) — puis refaites " +
			"la recette obligatoire de §15.5 : REDÉMARREZ la machine et cochez « le poste est " +
			"revenu seul sur l'écran client »."
	}
	return "Activez les deux unités (systemctl enable openscale.service openscale-kiosk.service), " +
		"puis refaites la recette de §15.5 : redémarrez la machine et vérifiez que le poste " +
		"revient seul sur l'écran client."
}

// --- 4. The data directory --------------------------------------------------

func (d *Doctor) checkDataDirectory() Control {
	control := Control{ID: ControlDataDirectory,
		Checked: "Droits d'écriture sur le répertoire de données"}
	if d.o.DataDir == "" {
		control.Status, control.Observed = StatusFail, "aucun répertoire de données n'a été désigné"
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
		control.Status, control.Observed = StatusUnknown, "aucun répertoire de données n'a été désigné"
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

// --- 6. The listening address -----------------------------------------------

// The two codes §13.4 allocates to a listening address that cannot be taken.
const (
	codeAnotherInstance = "ERR-SYS-01"
	codeCannotListen    = "ERR-SYS-02"
)

func (d *Doctor) checkListenAddress(ctx context.Context, loaded loadedConfig) Control {
	control := Control{ID: ControlListenAddress,
		Checked: "Adresse d'écoute libre, ou tenue par ce poste"}
	address := loaded.Config.Network.Listen
	if address == "" {
		control.Status, control.Observed = StatusFail, "aucune adresse d'écoute n'est déclarée (network.listen)"
		control.Remedy = "Renseignez network.listen dans " + or(d.o.ConfigPath, "la configuration") +
			", par exemple 127.0.0.1:8080, puis redémarrez le service."
		return control
	}

	state, err := d.o.Machine.CanListen(ctx, address)
	if err != nil || !state.Determined {
		control.Status = StatusUnknown
		control.Observed = fmt.Sprintf("l'adresse %s n'a pas pu être testée%s", address, detailSuffix(err))
		control.Remedy = "Vérifiez que network.listen s'écrit hôte:port, par exemple 127.0.0.1:8080."
		return control
	}
	if state.Bindable {
		control.Status = StatusPass
		control.Observed = fmt.Sprintf("%s est libre : le service pourra la prendre", address)
		return control
	}

	// The socket IS the single-instance lock (§13.4). An address that refuses a bind AND
	// answers our own /healthz is held by this very station, which is the nominal case
	// when the service is running — and not a fault to report.
	if live, err := d.liveness(ctx); err == nil && live.IsOpenScale() {
		control.Status = StatusPass
		control.Observed = fmt.Sprintf("%s est tenue par ce poste : /healthz répond (budget %d ms)",
			address, live.BudgetMS)
		return control
	}
	control.Status, control.Code = StatusFail, codeCannotListen
	control.Observed = fmt.Sprintf("%s est déjà prise, et ce qui la tient n'est pas OpenScale : %s",
		address, or(state.Detail, "le bind a été refusé"))
	control.Remedy = "Deux cas, et un seul geste pour les distinguer. Si un autre programme écoute " +
		"sur ce port, changez network.listen — 127.0.0.1:8081 par exemple. Si c'est une " +
		"instance d'OpenScale restée en vie (" + codeAnotherInstance + "), arrêtez le service " +
		"avant d'en lancer un second."
	return control
}

// liveness asks /healthz, and only to tell « held by us » from « held by something else ».
func (d *Doctor) liveness(ctx context.Context) (Liveness, error) {
	if d.o.Service == nil {
		return Liveness{}, ErrServiceSilent
	}
	return d.o.Service.Liveness(ctx)
}

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

// --- 10. The serial port ----------------------------------------------------

const codePortUnavailable = "ERR-SCL-03"

// optionPort is the key a SERIAL scale.options carries the port name under (§11.2).
//
// It is a literal and it is allowed to be one, now that the control runs only for a
// protocol that declares itself on a serial port: `port` is the key
// internal/scale/serial declares in its own option schema, and it exists exactly when
// this control applies. What it may no longer do — and did — is assume that every scale
// of every protocol is reached through it.
const optionPort = "port"

// scaleEndpoint reports what kind of access point the protocol id names is reached on,
// as the driver itself declared it, and whether this binary knows that protocol at all.
//
// An UNKNOWN protocol is not answered with a guess: control 8 already reports a
// scale.type no driver of this binary carries, and this control then does what it always
// did rather than adding a second, differently worded verdict on the same fault.
func (d *Doctor) scaleEndpoint(id string) (string, bool) {
	for _, descriptor := range d.o.Registries.Scales {
		if descriptor.ID == id {
			return descriptor.Endpoint, true
		}
	}
	return "", false
}

func (d *Doctor) checkSerialPort(ctx context.Context, loaded loadedConfig) Control {
	control := Control{ID: ControlSerialPort, Checked: "Port série présent et ouvrable"}
	if !loaded.Config.Scale.Present {
		// The explicit declaration of §11.2, which turns the light OFF instead of leaving
		// it red. It is not a fault and must not be reported as one.
		control.Status = StatusNotApplicable
		control.Observed = "ce poste est déclaré sans balance (scale.present = false) : la saisie du " +
			"poids à la main est le mode nominal"
		return control
	}
	if endpoint, known := d.scaleEndpoint(loaded.Config.Scale.Type); known &&
		endpoint != domain.EndpointSerialPort {
		// A protocol that is not reached through a serial port has no scale.options.port,
		// and this control would report a missing key as a fault on a station that is
		// perfectly configured. The light goes OFF, like the one of a station with no
		// scale, and says why.
		control.Status = StatusNotApplicable
		control.Observed = fmt.Sprintf("le protocole %s ne passe pas par un port série : "+
			"il n'y a pas de scale.options.port à vérifier sur ce poste", loaded.Config.Scale.Type)
		return control
	}
	declared, _ := loaded.Config.Scale.Options.Text(optionPort)
	if declared == "" {
		control.Status, control.Code = StatusFail, codePortUnavailable
		control.Observed = "aucun port n'est déclaré (scale.options.port) alors que ce poste annonce une balance"
		control.Remedy = "Ouvrez la page Matériel et lancez « Détecter automatiquement » : " +
			"la détection ouvre chaque port, applique les parseurs et annonce celui qui répond. " +
			"Ou déclarez scale.present = false si ce poste n'a réellement pas de balance."
		return control
	}

	list, err := d.o.Machine.SerialPorts(ctx)
	if err != nil {
		control.Status = StatusUnknown
		control.Observed = fmt.Sprintf("le port %s est déclaré, et les ports du poste n'ont pas pu "+
			"être énumérés : %v", declared, err)
		control.Remedy = "Relancez la commande depuis une invite administrateur, puis vérifiez le " +
			"câble de la balance."
		return control
	}
	if !containsPort(list, declared) {
		control.Status, control.Code = StatusFail, codePortUnavailable
		control.Observed = fmt.Sprintf("le port %s est déclaré et n'existe pas sur ce poste. %s",
			declared, portListSentence(list))
		control.Remedy = "Rebranchez le câble de la balance, puis relancez la commande. Si le port a " +
			"changé de nom — c'est ce qui arrive après un rebranchement — corrigez " +
			"scale.options.port, ou lancez « Détecter automatiquement » depuis Réglages " +
			"avancés → Matériel. Vérifiez aussi le contrôle 15 : la suspension USB sélective " +
			"fait disparaître un adaptateur USB-série."
		return control
	}

	if err := d.o.Machine.OpenSerialPort(ctx, declared); err != nil {
		// A port that is enumerated but refuses to open is EXCLUSIVE and held — which is
		// what a running service looks like from here, and a success rather than a fault.
		if live, liveErr := d.liveness(ctx); liveErr == nil && live.IsOpenScale() {
			control.Status = StatusPass
			control.Observed = fmt.Sprintf("le port %s existe et il est tenu par le service en cours : "+
				"un port série est exclusif, c'est le résultat attendu quand le poste tourne", declared)
			return control
		}
		control.Status, control.Code = StatusFail, codePortUnavailable
		control.Observed = fmt.Sprintf("le port %s existe et ne s'ouvre pas : %v", declared, err)
		control.Remedy = "Un port série est exclusif. Fermez ce qui le tient — un autre programme, " +
			"une fenêtre de terminal série restée ouverte — puis relancez la commande. Si " +
			"personne ne le tient, c'est un droit qui manque : le compte du service doit " +
			"appartenir au groupe dialout sous Linux (§15.3)."
		return control
	}
	control.Status = StatusPass
	control.Observed = fmt.Sprintf("le port %s existe et s'ouvre", declared)
	return control
}

// containsPort reports whether the declared name was enumerated.
//
// The comparison is case-insensitive because Windows spells the same port COM8 and com8,
// and a control that refused com8 would send somebody looking for a cable that is plugged
// in.
func containsPort(list []PortInfo, name string) bool {
	for _, port := range list {
		if strings.EqualFold(port.Name, name) {
			return true
		}
	}
	return false
}

// portListSentence names what WAS enumerated, which is the half of the remedy a volunteer
// can act on.
func portListSentence(list []PortInfo) string {
	if len(list) == 0 {
		return "Aucun port série n'est visible sur ce poste."
	}
	names := make([]string, 0, len(list))
	for _, port := range list {
		names = append(names, port.String())
	}
	return "Ports visibles : " + strings.Join(names, " · ") + "."
}

// --- 11. The print queue, from the service's context ------------------------

const codePrinterUnreachable = "ERR-PRN-01"

func (d *Doctor) checkPrintQueue(ctx context.Context, loaded loadedConfig, health Health, healthErr error) Control {
	control := Control{ID: ControlPrintQueue,
		Checked: "File d'impression visible depuis le contexte du service"}
	if healthErr != nil {
		control.Status = StatusUnknown
		control.Observed = "le service ne répond pas, et lui seul peut répondre : une file " +
			"« installée pour l'utilisateur » est invisible du service tout en étant parfaitement " +
			"visible d'ici. " + d.localQueues(ctx)
		control.Remedy = "Démarrez le service (contrôle 1), puis relancez openscale doctor. Ce " +
			"contrôle interroge le service exprès : le tester avec les droits de l'opérateur " +
			"répondrait à une autre question (§15.2, important-11)."
		return control
	}

	configured, _ := loaded.Config.Printer.Options.Text("queue")
	switch health.State.Printer.Health {
	case "faulted":
		control.Status, control.Code = StatusFail, codePrinterUnreachable
		control.Observed = fmt.Sprintf("le service ne peut pas imprimer : %s. %s",
			or(health.State.Printer.Detail, "aucun détail"), configuredQueueSentence(configured))
		control.Remedy = "Sous Windows, la file doit être installée en imprimante LOCALE MACHINE : " +
			"une file « installée pour l'utilisateur » est invisible depuis le service, et c'est " +
			"la panne la plus fréquente à l'installation (§15.2). " + d.localQueues(ctx) +
			"\nEn attendant, l'écran de dépannage propose « Imprimer sur l'imprimante du poste N »."
	case "consumable":
		control.Status = StatusWarn
		control.Observed = "le service imprime, et le rouleau arrive en fin de vie : " +
			or(health.State.Printer.Detail, "aucun détail")
		control.Remedy = "Changez le rouleau, puis touchez « J'ai changé le rouleau » sur l'écran " +
			"de dépannage — c'est ce bouton qui remet le compteur à zéro (§8.5)."
	case "unknown":
		control.Status = StatusPass
		control.Observed = "le service atteint l'imprimante ; celle-ci ne sait pas dire ce qu'elle a " +
			"— les octets partent, rien ne revient. C'est la réponse honnête d'un transport " +
			"unidirectionnel, pas une panne. " + configuredQueueSentence(configured)
	case "ready":
		control.Status = StatusPass
		control.Observed = "le service voit l'imprimante et elle n'a rien à signaler. " +
			configuredQueueSentence(configured)
	default:
		control.Status = StatusUnknown
		control.Observed = fmt.Sprintf("le service annonce un état d'imprimante que cette version ne "+
			"connaît pas : %q", health.State.Printer.Health)
		control.Remedy = "Les deux binaires ne sont pas de la même version. Mettez ce poste à jour, " +
			"puis relancez la commande."
	}
	return control
}

// configuredQueueSentence names what the configuration asks for.
func configuredQueueSentence(queue string) string {
	if queue == "" {
		return "Aucune file n'est nommée dans printer.options.queue."
	}
	return fmt.Sprintf("File configurée : « %s ».", queue)
}

// localQueues names the queues visible from THIS process, labelled as such.
//
// The label is not decoration: presenting the operator's list as the service's viewpoint
// is the exact mistake important-11 is about, and the list is only ever useful as the
// second half of a remedy.
func (d *Doctor) localQueues(ctx context.Context) string {
	list, err := d.o.Machine.PrintQueues(ctx)
	if err != nil {
		return "Les files visibles depuis cette session n'ont pas pu être énumérées : " + err.Error() + "."
	}
	if len(list) == 0 {
		return "Aucune file d'impression n'est visible depuis cette session."
	}
	names := make([]string, 0, len(list))
	for _, queue := range list {
		name := queue.Name
		if queue.Default {
			name += " (par défaut)"
		}
		names = append(names, name)
	}
	return "Files visibles depuis cette session — pas depuis celle du service : " +
		strings.Join(names, " · ") + "."
}

// --- 12. The observed scale cadence -----------------------------------------

const codeScaleLost = "ERR-SCL-02"

func (d *Doctor) checkScaleRate(loaded loadedConfig, health Health, healthErr error) Control {
	control := Control{ID: ControlScaleRate, Checked: "Cadence de la balance réellement observée"}
	if !loaded.Config.Scale.Present {
		control.Status = StatusNotApplicable
		control.Observed = "ce poste est déclaré sans balance (scale.present = false)"
		return control
	}
	if healthErr != nil {
		control.Status = StatusUnknown
		control.Observed = "le service ne répond pas : la cadence est ce que le poste a MESURÉ sur " +
			"les soixante-quatre derniers intervalles, elle ne se déduit d'aucun fichier"
		control.Remedy = "Démarrez le service (contrôle 1), laissez-le recevoir quelques trames, " +
			"puis relancez openscale doctor."
		return control
	}

	scale := health.State.Scale
	switch {
	case !scale.Connected:
		control.Status, control.Code = StatusFail, codeScaleLost
		control.Observed = "le service n'a plus de balance : le port était ouvert et il s'est tu"
		control.Remedy = "Vérifiez le câble et l'alimentation de la balance, puis rebranchez : le " +
			"poste revient à l'état nominal seul. En attendant, l'écran client propose la saisie " +
			"du poids à la main."
	case scale.Observations == 0:
		control.Status = StatusUnknown
		control.Observed = "le service tient le port et n'a encore reçu aucune trame"
		control.Remedy = "Posez quelque chose sur le plateau, attendez trois secondes, puis " +
			"relancez la commande. Si rien n'arrive, vérifiez le débit et la parité déclarés " +
			"dans scale.options contre ceux affichés sur la balance."
	case scale.TooSlow:
		// The alert condition itself is computed by the station, once, and read here:
		// expiry_factor × median above the ceiling (§6.5, ADR-005). Two implementations of
		// one rule is how the two of them come to disagree.
		control.Status = StatusWarn
		control.Observed = fmt.Sprintf("la balance émet une mesure toutes les %d ms, et le poids est "+
			"considéré périmé AVANT l'arrivée de la mesure suivante", scale.MedianMS)
		control.Remedy = "Le poids s'affichera puis disparaîtra sans raison visible. Vérifiez le " +
			"câble, puis la cadence d'émission réglée sur la balance elle-même : c'est un " +
			"réglage de l'appareil, pas du poste."
	case scale.Provisional:
		control.Status = StatusWarn
		control.Observed = fmt.Sprintf("cadence PROVISOIRE de %d ms sur %d intervalle(s) : moins de "+
			"huit ont été observés, la valeur affichée est celle que le driver déclare, pas une mesure",
			scale.MedianMS, scale.Observations)
		control.Remedy = "Laissez le poste recevoir des trames quelques secondes, puis relancez la " +
			"commande : le chiffre deviendra une mesure."
	default:
		control.Status = StatusPass
		control.Observed = fmt.Sprintf("une mesure toutes les %d ms, médiane mesurée sur %d intervalles",
			scale.MedianMS, scale.Observations)
	}
	return control
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

// --- 14. The system clock ---------------------------------------------------

const codeClockJump = "ERR-SYS-07"

func (d *Doctor) checkSystemClock(loaded loadedConfig) Control {
	control := Control{ID: ControlSystemClock, Checked: "Horloge système cohérente"}
	now := d.o.Clock.Now()
	control.Observed = "heure du poste : " + now.Format(clockLayout)

	built, builtKnown := parseBuildDate(d.o.BuildDate)
	written := loaded.Config.ModifiedAt

	switch {
	case builtKnown && now.Before(built):
		control.Status, control.Code = StatusFail, codeClockJump
		control.Observed += fmt.Sprintf(" — antérieure à la date de compilation du binaire (%s) : "+
			"l'horloge de ce poste est fausse", built.Format(clockLayout))
		control.Remedy = clockRemedy()
	case !written.IsZero() && now.Before(written):
		control.Status, control.Code = StatusFail, codeClockJump
		control.Observed += fmt.Sprintf(" — antérieure à la date d'écriture de la configuration (%s) : "+
			"l'horloge a reculé", written.Format(clockLayout))
		control.Remedy = clockRemedy()
	case !builtKnown:
		control.Status = StatusUnknown
		control.Observed += " — ce binaire ne porte pas sa date de compilation, il n'y a donc rien à comparer"
		control.Remedy = "Rien à faire sur le poste. Ce binaire a été construit sans le Makefile, " +
			"qui injecte la version, le commit et la date : reconstruisez-le avec `make build` " +
			"pour que ce contrôle puisse conclure."
	default:
		control.Status = StatusPass
		control.Observed += fmt.Sprintf(", postérieure à la compilation du binaire (%s)", built.Format(clockLayout))
	}
	return control
}

// clockLayout is how this report spells an instant: local time, seconds, and the offset.
//
// The offset is what makes an archive readable six months later, from another timezone,
// by somebody reconciling a weighing journal against a till.
const clockLayout = "2006-01-02 15:04:05 -07:00"

// clockRemedy is the instruction for a clock that is wrong.
//
// A timestamped journal is only worth anything for reconciliation with the till if the
// hour is right, and no NTP dependency is guaranteed on an offline station (§15.4).
func clockRemedy() string {
	return "Remettez l'heure du poste à la bonne date : un journal de pesées horodaté ne vaut " +
		"rien pour le rapprochement avec la caisse si l'heure est fausse, et le poste n'a " +
		"aucune garantie de serveur de temps puisqu'il est hors ligne. Vérifiez aussi la pile " +
		"de la carte mère : une heure qui revient toujours à la même date après une coupure, " +
		"c'est elle."
}

// parseBuildDate reads the instant the linker injected.
//
// The Makefile injects `git log -1 --format=%cI`, which is RFC 3339. A plain `go build`
// injects "unknown", and saying so is the honest answer — a control that treated an
// unparsable date as the zero instant would report every station's clock as being in the
// future.
func parseBuildDate(value string) (time.Time, bool) {
	if value == "" || value == "unknown" {
		return time.Time{}, false
	}
	built, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, false
	}
	return built, true
}

// --- 16. The right to restart the machine -----------------------------------

// checkRebootPermission is the sixteenth control: may this station restart the computer?
//
// It exists because the answer is INVISIBLE until somebody needs it. Under Linux the
// service runs as `openscale` and polkit stands between it and the right, so a station
// missing its rule works perfectly — right up to the evening a volunteer is facing a
// frozen kiosk, touches the one button that would have saved them, and watches a
// countdown expire on nothing.
func (d *Doctor) checkRebootPermission(ctx context.Context) Control {
	control := Control{ID: ControlRebootPermission,
		Checked: "Droit de redémarrer l'ordinateur depuis l'écran"}
	state, err := d.o.Machine.RebootPermission(ctx)
	switch {
	case err != nil:
		control.Status, control.Observed = StatusUnknown,
			"le droit de redémarrer n'a pas pu être établi : "+err.Error()
		control.Remedy = "Vérifiez à la main que /etc/polkit-1/rules.d porte la règle " +
			"49-openscale-reboot.rules, ou relancez « sudo ./install.sh »."
	case !state.Applicable:
		control.Status = StatusNotApplicable
		control.Observed = "ce système ne sait pas redémarrer depuis l'écran (§15.3), " +
			"il n'y a donc aucun droit à vérifier"
	case state.Allowed:
		control.Status, control.Observed = StatusPass, state.Detail
	default:
		control.Status, control.Code = StatusFail, codeRebootRefused
		control.Observed = "NON : " + state.Detail + ". Le bouton « Redémarrer l'ordinateur » " +
			"répondra « accès refusé », et il ne le dira qu'au moment où quelqu'un en a besoin."
		control.Remedy = "Relancez « sudo ./install.sh » depuis deploy/linux : il pose la " +
			"règle polkit qui autorise le compte du poste à redémarrer l'ordinateur, et rien d'autre."
	}
	return control
}

// codeRebootRefused is ERR-SYS-12, and internal/web allocates it to the same fact: the
// machine was asked to restart and said no.
const codeRebootRefused = "ERR-SYS-12"

// --- 17. The client screen cannot leave the application ---------------------

// checkNavigationLock is the seventeenth control, and it is the only one that reports a
// station where EVERYTHING ELSE IS GREEN.
//
// The panne, in full: a right click on the administration screen — the one surface where
// the context menu is deliberately left alive, so that « Copier » works on an error a
// volunteer is reading over the telephone — offers « Rechercher sur le web ». One click,
// and the kiosk window is on a search engine. No address bar, no back button, and the
// browser is perfectly alive: the service answers, the task is running, the window is full
// screen, and the poste sells nothing. It happened on a real station on 31/07/2026.
//
// What it reads is the belt, not the guarantee. The braces are the supervisor's watch over
// the attached client screen, which brings the poste back inside AbsenceGrace whatever the
// browser did with these keys — which is why an unreadable answer here is amber and never
// red.
func (d *Doctor) checkNavigationLock(ctx context.Context) Control {
	control := Control{ID: ControlNavigationLock,
		Checked: "Écran client verrouillé sur l'application"}
	state, err := d.o.Machine.NavigationLock(ctx)
	switch {
	case err != nil:
		control.Status, control.Observed = StatusUnknown,
			"les stratégies de navigation n'ont pas pu être lues : "+err.Error()
		control.Remedy = navigationLockRemedy
	case !state.Applicable:
		control.Status = StatusNotApplicable
		control.Observed = "sur ce système, l'écran client tourne sous un compositeur " +
			"mono-application (§15.3) et la stratégie du navigateur appartient à " +
			"l'installeur, pas au compte du poste"
	case !state.Determined:
		control.Status, control.Observed = StatusUnknown, state.Detail
		control.Remedy = navigationLockRemedy
	case state.Locked:
		control.Status = StatusPass
		control.Observed = "compte « " + state.Account + " » : " + state.Detail +
			" Un clic droit ne peut plus emmener le poste hors de l'application."
	default:
		control.Status, control.Code = StatusFail, codeNavigationOpen
		control.Observed = "compte « " + state.Account + " » : " + state.Detail +
			" Le navigateur peut être emmené hors de l'application — un clic droit, " +
			"« Rechercher sur le web », et il n'y a ni barre d'adresse ni bouton retour " +
			"pour revenir."
		control.Remedy = navigationLockRemedy
	}
	return control
}

// navigationLockRemedy is one gesture, and it is the same one for the three branches that
// carry it: the policies are posed by the kiosk at every logon, so making them exist again
// is making the kiosk start again.
const navigationLockRemedy = "Fermez puis rouvrez la session du poste — le kiosque pose " +
	"ses stratégies à chaque ouverture. Si le contrôle reste rouge, le journal " +
	"kiosk.log dit sur quelle clé il a échoué."

// codeNavigationOpen is ERR-KSK-03: the kiosk window can be taken out of the application.
//
// A code of its own, and the third of the kiosk family: ERR-KSK-02 says « l'affichage
// n'arrive pas à rester ouvert », which is the opposite failure, and reading one for the
// other over the telephone sends a volunteer to look at a browser that crashes when the
// browser is doing fine.
const codeNavigationOpen = "ERR-KSK-03"

// --- 15. Sleep and USB selective suspend ------------------------------------

func (d *Doctor) checkPowerSettings(ctx context.Context) Control {
	control := Control{ID: ControlPowerSettings,
		Checked: "Veille et suspension USB sélective désactivées"}
	state, err := d.o.Machine.Power(ctx)
	switch {
	case err != nil:
		control.Status, control.Observed = StatusUnknown, "les réglages d'énergie n'ont pas pu être lus : "+err.Error()
		control.Remedy = "Relancez cette commande depuis une invite administrateur."
	case !state.Applicable:
		control.Status = StatusNotApplicable
		control.Observed = "la procédure d'installation de ce système n'écrit aucun réglage " +
			"d'énergie (§15.3), il n'y a donc rien à comparer"
	case !state.Determined:
		control.Status, control.Observed = StatusUnknown, "les réglages d'énergie n'ont pas pu être établis : "+state.Detail
		control.Remedy = "Vérifiez à la main, dans le plan d'alimentation actif, que la mise en " +
			"veille, l'extinction de l'écran et la « suspension sélective USB » sont toutes sur " +
			"« jamais » ou « désactivé » (§15.2, étape 5)."
	case !state.USBSelectiveSuspendDisabled:
		control.Status = StatusFail
		control.Observed = "la suspension USB sélective est ACTIVE. " + state.Detail
		control.Remedy = "C'est la cause de la moitié des « la balance ne répond plus » sur un " +
			"adaptateur USB-série, et elle ne figure dans aucune procédure d'installation " +
			"standard. En administrateur :\n" + usbSuspendCommand() +
			"\nOu relancez install.ps1, dont c'est l'étape 5 (§15.2)."
	case !state.SleepDisabled:
		control.Status = StatusFail
		control.Observed = "la mise en veille ou l'extinction de l'écran est ACTIVE. " + state.Detail
		control.Remedy = "Un poste en libre-service ne doit ni s'endormir ni éteindre son écran. " +
			"En administrateur :\npowercfg /change monitor-timeout-ac 0\n" +
			"powercfg /change standby-timeout-ac 0\npowercfg /change hibernate-timeout-ac 0"
	default:
		control.Status = StatusPass
		control.Observed = "veille, extinction d'écran et suspension USB sélective sont toutes désactivées"
	}
	return control
}

// usbSuspendCommand is the command of §15.2, GUIDs included, quoted from the document.
//
// The two GUIDs are NOT derived and NOT guessed: they are copied from install.ps1 in
// §15.2, which is the only place this project has them from.
func usbSuspendCommand() string {
	return "powercfg /setacvalueindex SCHEME_CURRENT " + usbSubgroupGUID + " " + usbSuspendGUID + " 0\n" +
		"powercfg /setactive SCHEME_CURRENT"
}

// --- Shared -----------------------------------------------------------------

// detailSuffix appends the technical tail of an error, or nothing.
func detailSuffix(err error) string {
	if err == nil {
		return ""
	}
	return " : " + err.Error()
}
