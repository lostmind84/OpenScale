package diag

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"openscale/internal/domain"
	"openscale/internal/fake"
)

// The doubles of this file exist so that the controls can be driven, green case AND
// red case, on a machine that has no service, no scheduled task, no registry, no printer and
// no station running. That is the whole point: a diagnosis whose verdicts could only be
// exercised on an installed station would be a diagnosis nobody ever tested on the mornings
// it is for.

// benchEpoch is the instant every test of this package is frozen at.
var benchEpoch = time.Date(2026, 7, 25, 10, 30, 0, 0, time.UTC)

// benchBuildDate is the build date the linker would have injected, before benchEpoch.
const benchBuildDate = "2026-07-20T08:00:00Z"

// bench is one station under diagnosis, nominal until a test spoils exactly one thing.
type bench struct {
	t          *testing.T
	clock      *fake.Clock
	machine    *fakeMachine
	service    *fakeService
	base       *fakeDatabase
	openErr    error
	migrations int

	dataDir    string
	configPath string
	cfg        domain.Config
	registries domain.Registries
}

// newBench builds a station on which the controls all come out green.
//
// It is a REAL temporary data directory and a REAL configuration file: the write-rights
// control writes, and a double that pretended to would test nothing.
func newBench(t *testing.T) *bench {
	t.Helper()
	root := t.TempDir()
	b := &bench{
		t:          t,
		clock:      fake.NewClock(benchEpoch),
		machine:    newFakeMachine(),
		service:    newFakeService(),
		base:       &fakeDatabase{path: filepath.Join(root, "openscale.db"), schema: 1},
		migrations: 1,
		dataDir:    filepath.Join(root, "data"),
		configPath: filepath.Join(root, "config.json"),
		cfg:        benchConfig(),
		// Only the preview printer is declared, which is what the neutral profile of §11.5
		// names. An empty registry validates the FORM only, and the configuration control
		// says so rather than pretending it checked the driver values.
		registries: domain.Registries{
			Printers: []domain.DriverDescriptor{{ID: domain.PrinterPreview, Label: "Aperçu"}},
		},
	}
	if err := os.MkdirAll(b.dataDir, 0o755); err != nil {
		t.Fatalf("préparation du répertoire de données : %v", err)
	}
	return b
}

// benchConfig is the neutral profile of §11.5, made into a real station.
//
// The shipped profile and not a literal of its own: a test that invents a configuration
// proves nothing about the station anybody will run.
func benchConfig() domain.Config {
	cfg := domain.NeutralProfile()
	cfg.Station.Number = 2
	cfg.Station.Name = "Poste 2 — fruits"
	cfg.Station.Coop = "La Cagette"
	cfg.Admin.PasswordHash = benchPasswordHash
	cfg.Admin.RecoveryCodeHash = benchRecoveryHash
	cfg.ModifiedAt = benchEpoch.Add(-24 * time.Hour)
	return cfg
}

// The two argon2id strings of the delivered configuration file. They are the shape §11.2
// validates, and the shape the archive must never carry.
const (
	benchPasswordHash = "$argon2id$v=19$m=65536,t=3,p=2$b3BlbnNjYWxlLXNhbHQxMg$AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8"
	benchRecoveryHash = "$argon2id$v=19$m=65536,t=3,p=2$cmVjb3Zlcnktc2FsdC0wMQ$AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8"
)

// tweak edits the configuration before it is written to disk.
func (b *bench) tweak(edit func(*domain.Config)) *bench {
	edit(&b.cfg)
	return b
}

// withScale gives the station a scale on COM8, which the fake machine enumerates.
func (b *bench) withScale() *bench {
	return b.tweak(func(cfg *domain.Config) {
		cfg.Scale.Present = true
		cfg.Scale.Type = "gram-xfoc-rs"
		cfg.Scale.Options = domain.DriverOptions{optionPort: json.RawMessage(`"COM8"`)}
	})
}

// run writes the configuration and performs the controls.
func (b *bench) run() Report {
	b.t.Helper()
	b.writeConfig()
	doctor, err := New(b.options())
	if err != nil {
		b.t.Fatalf("construction du doctor : %v", err)
	}
	report := doctor.Run(context.Background())
	if err := report.Validate(); err != nil {
		// The rule of §15.4, enforced on every single run of every single test: a verdict
		// that is not green MUST say what to do.
		b.t.Fatalf("le rapport se contredit lui-même :\n%v", err)
	}
	return report
}

// options is what the doctor is given.
func (b *bench) options() Options {
	o := Options{
		Clock: b.clock, ConfigPath: b.configPath, DataDir: b.dataDir,
		Version: "1.0.0-test", Commit: "abc1234", BuildDate: benchBuildDate,
		Machine: b.machine, Service: b.service, Registries: b.registries,
		Migrations: b.migrations,
	}
	o.OpenDatabase = func(string) (Database, error) {
		if b.openErr != nil {
			return nil, b.openErr
		}
		return b.base, nil
	}
	return o
}

// writeConfig writes the configuration where the doctor will read it.
func (b *bench) writeConfig() {
	b.t.Helper()
	raw, err := json.MarshalIndent(b.cfg, "", "  ")
	if err != nil {
		b.t.Fatalf("sérialisation de la configuration : %v", err)
	}
	if err := os.WriteFile(b.configPath, raw, 0o644); err != nil {
		b.t.Fatalf("écriture de la configuration : %v", err)
	}
}

// control returns one control of a report, or fails the test.
func control(t *testing.T, report Report, id string) Control {
	t.Helper()
	found, ok := report.Control(id)
	if !ok {
		t.Fatalf("le rapport ne porte aucun contrôle %q", id)
	}
	return found
}

// --- The doubles ------------------------------------------------------------

// fakeMachine is a host that answers whatever a test tells it to.
type fakeMachine struct {
	service       ServiceState
	serviceErr    error
	kiosk         ServiceState
	kioskErr      error
	autoLogon     AutoLogonState
	autoLogonErr  error
	power         PowerState
	powerErr      error
	serialPorts   []PortInfo
	portsErr      error
	openPortErr   error
	queues        []QueueInfo
	queuesErr     error
	space         FreeSpace
	spaceErr      error
	listen        ListenState
	listenErr     error
	system        SystemInfo
	reboot        RebootPermissionState
	rebootErr     error
	navigation    NavigationLockState
	navigationErr error
}

// newFakeMachine returns a host on which nothing is wrong.
func newFakeMachine() *fakeMachine {
	return &fakeMachine{
		service: ServiceState{Name: "OpenScale", Known: true, Running: true, Automatic: true,
			Determined: true, Detail: "RUNNING, AUTO_START"},
		kiosk: ServiceState{Name: "OpenScale-Kiosk", Known: true, Determined: true, Detail: "Prêt"},
		autoLogon: AutoLogonState{Enabled: true, Account: "openscale", Expected: "openscale",
			Determined: true, Detail: "AutoAdminLogon = 1."},
		power: PowerState{SleepDisabled: true, USBSelectiveSuspendDisabled: true,
			Applicable: true, Determined: true},
		serialPorts: []PortInfo{{Name: "COM8", Description: "FTDI FT232R", VID: "0403", PID: "6001"}},
		queues:      []QueueInfo{{Name: "SATO WS408_2", Detail: "file locale de la machine", Default: true}},
		space:       FreeSpace{FreeBytes: 5 << 30, TotalBytes: 120 << 30, Determined: true},
		listen:      ListenState{Address: "127.0.0.1:8085", Bindable: true, Determined: true},
		system: SystemInfo{OS: "windows", Arch: "amd64", Hostname: "PESEE-2",
			Uptime: 30 * time.Hour, UptimeText: "30 h"},
		// A nominal station may restart the machine. The question is ASKED of this
		// double and not of the system the test runs on: reading the real one made the
		// nominal bench green on a developer's Windows and red on the CI runner, which
		// has no polkit rule and never will.
		reboot: RebootPermissionState{Allowed: true, Applicable: true,
			Detail: "le service tourne en LocalSystem, qui porte le privilège d'arrêt"},
		// Un poste nominal est verrouillé sur son application : les stratégies que le
		// kiosque pose à chaque ouverture de session sont en place sous le compte du poste.
		navigation: NavigationLockState{Locked: true, Applicable: true, Determined: true,
			Account: "openscale", Browser: "Microsoft Edge",
			Detail: "Microsoft Edge : URLBlocklist = *."},
	}
}

func (m *fakeMachine) Service(context.Context) (ServiceState, error) {
	return m.service, m.serviceErr
}
func (m *fakeMachine) KioskTask(context.Context) (ServiceState, error) { return m.kiosk, m.kioskErr }
func (m *fakeMachine) AutoLogon(context.Context) (AutoLogonState, error) {
	return m.autoLogon, m.autoLogonErr
}
func (m *fakeMachine) Power(context.Context) (PowerState, error) { return m.power, m.powerErr }
func (m *fakeMachine) RebootPermission(context.Context) (RebootPermissionState, error) {
	return m.reboot, m.rebootErr
}
func (m *fakeMachine) NavigationLock(context.Context) (NavigationLockState, error) {
	return m.navigation, m.navigationErr
}
func (m *fakeMachine) SerialPorts(context.Context) ([]PortInfo, error) {
	return m.serialPorts, m.portsErr
}
func (m *fakeMachine) OpenSerialPort(context.Context, string) error { return m.openPortErr }
func (m *fakeMachine) PrintQueues(context.Context) ([]QueueInfo, error) {
	return m.queues, m.queuesErr
}
func (m *fakeMachine) FreeSpace(path string) (FreeSpace, error) {
	space := m.space
	space.Path = path
	return space, m.spaceErr
}
func (m *fakeMachine) CanListen(context.Context, string) (ListenState, error) {
	return m.listen, m.listenErr
}
func (m *fakeMachine) Describe(context.Context) SystemInfo { return m.system }

// fakeService is a running station that answers whatever a test tells it to.
type fakeService struct {
	health    Health
	healthErr error
	liveness  Liveness
	liveErr   error
}

// newFakeService returns a station on which nothing is wrong.
func newFakeService() *fakeService {
	health := Health{
		Version: "1.0.0-test", Fingerprint: "deadbeef", Station: 2, Alive: true,
		State: healthState{State: "idle", CatalogCount: 331,
			Scale:   healthScale{Connected: true, MedianMS: 400, Observations: 64},
			Printer: healthPrinter{Health: "ready", ObservedAt: benchEpoch.Format(time.RFC3339)}},
		Counters: healthCounters{Journal: 128},
		Catalog: &HealthImport{OccurredAt: benchEpoch.Format(time.RFC3339),
			Source: domain.CatalogSourceLocalDrop, FileName: "flv_2.csv",
			Result: domain.ImportApplied, RowsRead: 355, Weighable: 331,
			NotWeighable: 8, Anomalies: 16},
	}
	raw, _ := json.Marshal(health)
	health.Raw = raw
	return &fakeService{health: health, liveness: Liveness{Alive: true, BudgetMS: 500}}
}

func (s *fakeService) Health(context.Context) (Health, error) { return s.health, s.healthErr }
func (s *fakeService) Liveness(context.Context) (Liveness, error) {
	return s.liveness, s.liveErr
}

// silence makes the station stop answering, which is what a service that will not start
// looks like from here.
func (s *fakeService) silence() {
	s.healthErr = errors.New("le service ne répond pas : connexion refusée")
	s.liveErr = ErrServiceSilent
}

// fakeDatabase is a station base that answers whatever a test tells it to.
type fakeDatabase struct {
	path         string
	schema       int
	schemaErr    error
	integrityErr error
	closed       bool
}

func (d *fakeDatabase) Path() string                         { return d.path }
func (d *fakeDatabase) SchemaVersion() (int, error)          { return d.schema, d.schemaErr }
func (d *fakeDatabase) IntegrityCheck(context.Context) error { return d.integrityErr }
func (d *fakeDatabase) Close() error                         { d.closed = true; return nil }

// reportHead renders the report and returns its first line.
func reportHead(t *testing.T, report Report) string {
	t.Helper()
	out := &strings.Builder{}
	if err := report.WriteText(out); err != nil {
		t.Fatalf("rendu du rapport : %v", err)
	}
	return out.String()
}
