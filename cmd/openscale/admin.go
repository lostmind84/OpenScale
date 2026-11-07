package main

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"openscale/internal/diag"
	"openscale/internal/domain"
	"openscale/internal/platform"
	"openscale/internal/printing"
	"openscale/internal/station"
	"openscale/internal/station/ports"
	"openscale/internal/store"
	"openscale/internal/web"
)

// THIS FILE MAKES THE ADMINISTRATION ROUTES REAL.
//
// internal/web declares what it needs — a Store, a ConfigStore, a CatalogAdmin, a
// Hardware, a SelfTester, a Troubleshooting — and every one of them is declared ON THE
// CONSUMER'S SIDE (cut 3 of §5.2): the HTTP layer names no database, no serial port and
// no print queue. What that costs is the handful of adapters below, and what it buys is
// that a route answers 501 « fonction non disponible sur ce poste » instead of failing
// when a station is wired without one of them.
//
// The two holders at the end of the file exist for a reason worth reading before the
// rest: a reload REPLACES the printer and the catalog source (§11.4), so an adapter that
// captured either at start-up would, twenty minutes into a service, be changing the roll
// counter of a printer nobody prints on any more.

// adminStore is the persistence as the administration screens read it.
//
// Ten methods, each one a translation between two structures that carry the same values.
// That is the price of cut 3 and it is paid here, in the composition root, rather than by
// letting internal/web import internal/store.
type adminStore struct{ db *store.DB }

// Weighings returns one page of the weighing journal, most recent first.
func (s adminStore) Weighings(ctx context.Context, q web.JournalQuery) ([]domain.Weighing, error) {
	return s.db.Weighings(ctx, store.JournalFilter{
		Since: q.Since, Until: q.Until, Result: q.Result,
		Limit: q.Limit, Offset: q.Offset,
	})
}

// CountWeighings reports how many rows the journal holds, which is the figure the
// dashboard shows next to the unlogged-weighing counter.
func (s adminStore) CountWeighings(ctx context.Context) (int, error) {
	return s.db.CountWeighings(ctx)
}

// TechnicalEntries returns one page of the technical journal.
func (s adminStore) TechnicalEntries(ctx context.Context, q web.TechnicalQuery) ([]web.TechnicalLine, error) {
	entries, err := s.db.TechnicalEntries(ctx, store.TechnicalFilter{
		Since: q.Since, Until: q.Until, Level: q.Level,
		Source: q.Source, Code: q.Code, Limit: q.Limit, Offset: q.Offset,
	})
	if err != nil {
		return nil, err
	}
	out := make([]web.TechnicalLine, 0, len(entries))
	for _, entry := range entries {
		out = append(out, web.TechnicalLine{
			ID: entry.ID, OccurredAt: entry.OccurredAt, Level: entry.Level,
			Source: entry.Source, Code: entry.Code,
			Message: entry.Message, Detail: entry.Detail,
		})
	}
	return out, nil
}

// Imports returns the history of catalog imports, most recent first. The first row is
// what the dashboard reads its one-line inventory from (§14.4).
func (s adminStore) Imports(ctx context.Context, limit, offset int) ([]domain.Import, error) {
	return s.db.Imports(ctx, limit, offset)
}

// LastAppliedImport returns the most recent import that put a catalog in service, which
// is the row the screens read when the last one changed nothing (§14.4, ADR-053).
func (s adminStore) LastAppliedImport(ctx context.Context) (domain.Import, error) {
	return s.db.LastAppliedImport(ctx)
}

// Findings returns what one import had to say about the rows it read.
func (s adminStore) Findings(ctx context.Context, importID int64) ([]domain.Finding, error) {
	return s.db.Findings(ctx, importID)
}

// LocalDecisions returns the human judgements in force (§10.6).
func (s adminStore) LocalDecisions(ctx context.Context) ([]domain.LocalDecision, error) {
	return s.db.LocalDecisions(ctx)
}

// SaveDecision records one human judgement about one product.
func (s adminStore) SaveDecision(ctx context.Context, d domain.LocalDecision) error {
	return s.db.SaveDecision(ctx, d)
}

// ClearDecision removes the judgement about one product.
func (s adminStore) ClearDecision(ctx context.Context, productID string) error {
	return s.db.ClearDecision(ctx, productID)
}

// Image returns the metadata of one photo, addressed by its content (§10.7).
func (s adminStore) Image(ctx context.Context, sha string) (domain.Image, error) {
	return s.db.Image(ctx, sha)
}

// adminConfig is the configuration FILE as the administration screen writes it.
//
// The rotation of the five versions and the atomic write live in internal/platform,
// where the file system does: this type only converts one structure into another.
type adminConfig struct{ file *platform.ConfigStore }

// Read returns the configuration as it stands ON DISK, which the expert pages edit and
// a rescue writes back — never the one in force (§11.3, §11.4).
func (c adminConfig) Read(ctx context.Context) (domain.Config, error) {
	return c.file.Read(ctx)
}

// Save rotates the versions and writes atomically (§11.4, steps 3 and 4).
func (c adminConfig) Save(ctx context.Context, cfg domain.Config) error {
	return c.file.Save(ctx, cfg)
}

// Versions lists the restorable versions, most recent first.
func (c adminConfig) Versions(ctx context.Context) ([]web.ConfigVersion, error) {
	versions, err := c.file.Versions(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]web.ConfigVersion, 0, len(versions))
	for _, v := range versions {
		out = append(out, web.ConfigVersion{
			Version: v.Version, ModifiedAt: v.ModifiedAt, Fingerprint: v.Fingerprint,
		})
	}
	return out, nil
}

// Restore reads back one version WITHOUT applying it.
func (c adminConfig) Restore(ctx context.Context, version int) (domain.Config, error) {
	return c.file.Restore(ctx, version)
}

// adminTroubleshooting is the three repair switches of §14.4, and not one of them writes
// the configuration file.
//
// That was the criterion of ADR-018 and it no longer decides who may press them: ADR-033
// asks what an act CHANGES, so ManualEntry sits behind the password while the roll counter
// and the fallback printer stay open. Which is which lives in the route table of
// internal/web, and nowhere else.
type adminTroubleshooting struct {
	station *station.Station
	printer *livePrinter
	file    *platform.ConfigStore
}

// ManualEntry switches the station into, or out of, manual weight entry.
//
// Coming BACK needs the scale block as the FILE declares it, because the configuration
// in memory no longer carries it — that is the whole point of manual entry being a state
// and not a saved setting (§11.4). A file that cannot be read is therefore a refusal on
// the way back, in French, and never on the way in: switching TO manual entry must work
// on a station whose configuration is unreadable, which is precisely the morning
// somebody needs it.
// It needs the `scale` block and NOTHING else, so a fault in another block must not stop
// it. That distinction is not a nicety: this station runs under Assigned Access, where
// opening config.json is not a gesture anybody has, and a fault on `pricing` would have
// locked a volunteer INSIDE manual weight entry with no way back and no way to repair the
// file either.
func (t adminTroubleshooting) ManualEntry(ctx context.Context, on bool) error {
	var asked domain.Config
	if !on {
		read, err := configForComingBack(ctx, t.file)
		if err != nil {
			// The French sentence and not %w: this one lands on the screen of a volunteer
			// standing at the station, and a typed error's Error() is English.
			return fmt.Errorf("la configuration de ce poste ne peut pas être relue, "+
				"la balance déclarée est donc inconnue : %s", readFailure(t.file.Path(), err))
		}
		asked = read
	}
	if err := t.station.ManualEntry(on, asked); err != nil {
		if errors.Is(err, station.ErrNoScaleToComeBackTo) {
			return errors.New("ce poste est déclaré sans balance : la saisie manuelle du poids " +
				"est son mode normal, il n'y a rien à rebasculer")
		}
		return err
	}
	return nil
}

// configForComingBack reads the file for the ONE block coming back from manual weight
// entry needs, and tolerates a fault in any other.
//
// It is the third of the three answers domain.UnreadableBlocksError exists for, and the
// one nothing else in the tree gives: a caller that needs a single block must not be
// stopped by a fault elsewhere. The cost of getting it wrong is not theoretical — this
// station runs under Assigned Access, where opening config.json is not a gesture anybody
// has, so a fault on `pricing` would leave a volunteer locked INSIDE manual entry with no
// way back and no way to repair the file either.
//
// A fault on `scale` itself is a genuine refusal: the neutral profile declares no scale,
// and coming back to one this station never declared is how a station ends up polling a
// serial port that is not there.
func configForComingBack(ctx context.Context, file *platform.ConfigStore) (domain.Config, error) {
	cfg, err := file.Read(ctx)
	var unreadable *domain.UnreadableBlocksError
	if errors.As(err, &unreadable) && !unreadable.Names("scale") {
		return unreadable.Config, nil
	}
	return cfg, err
}

// RollChanged puts the label counter back to zero (§8.5, « J'ai changé le rouleau »).
func (t adminTroubleshooting) RollChanged(ctx context.Context) error {
	service, err := t.printer.printService()
	if err != nil {
		return err
	}
	return service.Roll().Changed(ctx)
}

// UseFallbackPrinter routes printing to the neighbouring station's printer, or back
// (§8.4, bloquant-8).
//
// Both directions are asked for and neither is automatic: nothing observable tells this
// station that the main printer has been fixed, and an automatic switch would move a
// customer's label two metres away on a cable knocked loose for two seconds.
func (t adminTroubleshooting) UseFallbackPrinter(ctx context.Context, on bool) error {
	service, err := t.printer.printService()
	if err != nil {
		return err
	}
	if on {
		return service.UseFallback(ctx)
	}
	return service.UseMain(ctx)
}

// livePrinter is the printer IN SERVICE, which a configuration reload replaces (§11.4).
//
// Without it the roll counter and the fallback switch would act on the printer this
// process started with. That is not a theoretical concern: changing printer.options
// rebuilds the print service, and « J'ai changé le rouleau » would then reset a counter
// nothing increments any more.
type livePrinter struct {
	mu sync.RWMutex
	// printer is whatever the station prints through, including the stand-in a station
	// gets when its configuration names a printer this binary cannot build.
	printer ports.Printer
	// wrapper is the same object when the printer IS the print service, and nil
	// otherwise. The roll counter and the fallback routing belong to that type and to no
	// interface: they are what the station ADDS around a driver, and the Hub deliberately
	// knows nothing about them.
	wrapper *printing.Service
	// subject is WHAT the printer diagnostic runs against — the transport and the
	// device a reload put in service, computed once by diagnosisSubject and held next
	// to the printer it describes so a diagnostic run after a reload names the printer
	// that reload just built, never the one the process started with (§11.4).
	subject printing.Subject
}

var _ web.PrinterDiagnostician = (*livePrinter)(nil)

// hold puts the printer that has just taken service in place, with the Subject that
// describes it to the diagnostic — under the SAME lock, so a reader never sees a
// printer paired with the Subject of the one it replaced.
func (l *livePrinter) hold(printer ports.Printer, subject printing.Subject) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.printer = printer
	l.subject = subject
	l.wrapper, _ = printer.(*printing.Service)
}

// SelfTest prints one of the three built-in patterns of §8.6, on the printer in service.
//
// It satisfies web.SelfTester, which is one method wide on purpose: that is all the
// three self-test routes need, and a station whose printer could not be built still
// answers — with the reason the driver gives, which names the offending key.
// web.SelfTester stays at `error` (ADR-065, écart 1) — its one consumer, this method's
// caller, has no use for the receipt — so the receipt ports.Printer.SelfTest now
// returns is adapted away here, not threaded through.
func (l *livePrinter) SelfTest(ctx context.Context, what string) error {
	l.mu.RLock()
	printer := l.printer
	l.mu.RUnlock()
	if printer == nil {
		return errors.New("aucune imprimante n'est en service sur ce poste")
	}
	_, err := printer.SelfTest(ctx, what)
	return err
}

// errNoPrintService is the refusal every reader of l.wrapper gives when it is nil: a
// station whose printer.options name a queue this binary cannot open runs on the
// stand-in of serve.go, and the honest answer to « change the roll » — or to diagnose
// the printer — there is that there is no printer to act on.
//
// The refusal names a PAGE and no configuration key, for the reason the twin refusal of
// catalogadmin.go gives: whoever reads it has the administration screen in front of
// them, and the « réglages avancés » it used to point at were removed on 27/07/2026.
// « Pilote d'impression » is the French name the screen itself gives printer.type, so
// the sentence and the field it blames read the same. It is a shared value, not a
// sentence repeated at each call site, so printService and diagnosisTarget can never
// drift into naming the fault two different ways.
var errNoPrintService = errors.New("l'imprimante de ce poste n'a pas pu être construite : " +
	"reprenez le pilote d'impression et ses réglages sur la page Matériel")

// printService reports the print service, or says in French why there is none.
func (l *livePrinter) printService() (*printing.Service, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.wrapper == nil {
		return nil, errNoPrintService
	}
	return l.wrapper, nil
}

// diagnosisTarget reads the wrapper and the Subject that describes it to the
// diagnostic under the SAME RLock, for the guarantee hold's godoc states: a reader
// must never see a wrapper paired with the Subject of the printer it replaced. Reading
// them through two separate locks — one via printService, one of its own — would open
// exactly that window across a reload landing between the two.
func (l *livePrinter) diagnosisTarget() (*printing.Service, printing.Subject, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.wrapper == nil {
		return nil, printing.Subject{}, errNoPrintService
	}
	return l.wrapper, l.subject, nil
}

// DiagnosePrinter runs the printer diagnostic of page Matériel on the printer IN
// SERVICE, never on a second handle opened for the occasion (anti-objectif n°4):
// printing.Diagnose only ever calls l.wrapper.Interrogate and l.wrapper.SelfTest, the
// same two calls §8.5 and §8.6 already make on this same object. wrapper and subject
// come from diagnosisTarget's single RLock, so a reload cannot land between the two
// reads and pair the Subject it just built with the wrapper it just replaced.
//
// It refuses in the same two cases printService and UseFallback already name in
// French: no printer at all (same sentence as printService, admin.go) and the station
// on its fallback printer — diagnosing the neighbour's printer through THIS station's
// Subject would name the wrong device in every sentence steps 1 to 4 produce.
func (l *livePrinter) DiagnosePrinter(ctx context.Context) (web.PrinterDiagnosis, error) {
	wrapper, subject, err := l.diagnosisTarget()
	if err != nil {
		return web.PrinterDiagnosis{}, err
	}
	routing := wrapper.Routing()
	if routing.Fallback {
		return web.PrinterDiagnosis{}, errors.New("les étiquettes partent sur l'imprimante de secours : " +
			"repassez sur l'imprimante du poste, page Dépannage, avant de la diagnostiquer")
	}
	subject.Target = routing.Name

	steps := printing.Diagnose(ctx, subject, printing.DiagnosisProbes{
		Printer: wrapper,
		Queues:  diagnosisQueues,
	})
	return web.PrinterDiagnosis{Target: subject.Target, Steps: webDiagnosisSteps(steps)}, nil
}

// AnswerStep turns a volunteer's own answer to step 5 into the step itself (decision
// 3): nothing on this binary measures whether a label came out, so the only verdict
// that can exist here is the one a person gives.
//
// It reads the transport IN SERVICE under the same lock hold writes it with, so the
// remedy of a « Non » names what the bytes of step 4 actually reached — the Windows
// spooler is not an USB link (final review, I2) — while the route itself stays
// stateless: nothing of the run is kept between the two calls, only the printer the
// station holds anyway.
func (l *livePrinter) AnswerStep(printed bool, seen string) web.DiagnosisStep {
	l.mu.RLock()
	transport := l.subject.Transport
	l.mu.RUnlock()
	return webDiagnosisStep(printing.AnswerStep(transport, printed, seen))
}

// diagnosisQueues lists the Windows print queues the winspool step of the diagnostic
// checks a configured queue name against, filtered to the printer destinations
// (domain.DeviceKeyQueue) — platform.PrintQueues also enumerates serial-looking
// addresses the winspool step has no use for.
func diagnosisQueues(ctx context.Context) ([]string, error) {
	found, err := platform.PrintQueues(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(found))
	for _, q := range found {
		if q.Key == domain.DeviceKeyQueue {
			names = append(names, q.Name)
		}
	}
	return names, nil
}

// webDiagnosisStep copies one printing.DiagnosisStep across the package boundary
// §5.2 draws between internal/printing and internal/web.
func webDiagnosisStep(s printing.DiagnosisStep) web.DiagnosisStep {
	return web.DiagnosisStep{
		Number:   s.Number,
		Title:    s.Title,
		State:    string(s.State),
		Observed: s.Observed,
		Remedy:   s.Remedy,
		Doc:      s.Doc,
	}
}

// webDiagnosisSteps copies every step of one diagnostic run.
func webDiagnosisSteps(steps []printing.DiagnosisStep) []web.DiagnosisStep {
	out := make([]web.DiagnosisStep, len(steps))
	for i, s := range steps {
		out[i] = webDiagnosisStep(s)
	}
	return out
}

// adminDashboard answers the four questions of §14.4 that the HTTP layer cannot put to
// itself: the roll, the disk, the unattended restart and what the catalog source watches.
//
// It reads the two HOLDERS and never a value captured at start-up, for the reason the head
// of this file gives: a reload replaces the printer and the source, and a dashboard drawing
// the counter of a printer nobody prints on would be worse than no counter at all.
type adminDashboard struct {
	printer *livePrinter
	catalog *liveCatalog
	machine diag.Machine
	dataDir string

	// restart is evaluated ONCE and then answered from memory. §14.4 says the service
	// re-evaluates the three conditions « à chaque démarrage », which is what one
	// evaluation per process gives — and it is the reason this is not re-read on every
	// refresh: the question costs a reg.exe and a schtasks on Windows, and the answer
	// cannot change while the machine is up without somebody logging in to change it.
	once    sync.Once
	restart web.RestartReadiness
}

// Dashboard reports what it could establish, and leaves out what it could not.
func (d *adminDashboard) Dashboard(ctx context.Context) web.DashboardFacts {
	return web.DashboardFacts{
		Roll:    d.roll(),
		Disk:    d.disk(),
		Restart: d.unattendedRestart(ctx),
		Source:  d.source(),
		Routing: d.routing(),
	}
}

// routing reports which printer the labels come out of, and whether a fallback exists.
func (d *adminDashboard) routing() *web.PrintRouting {
	service, err := d.printer.printService()
	if err != nil {
		return nil
	}
	current := service.Routing()
	return &web.PrintRouting{
		Available: current.Available, OnFallback: current.Fallback,
		Name: current.Name, Banner: current.Banner,
	}
}

// roll reports the label counter of the printer IN SERVICE, or nothing at all.
//
// Nothing at all on a station whose printer could not be built: there is no roll to
// describe, and a counter shown at zero would read as a fresh roll.
func (d *adminDashboard) roll() *web.RollGauge {
	service, err := d.printer.printService()
	if err != nil {
		return nil
	}
	state := service.Roll().State()
	return &web.RollGauge{
		Printed: state.Printed, Capacity: state.Capacity, Remaining: state.Remaining,
		Level: state.Level, Message: state.Message, Known: state.Known,
	}
}

// disk reports the room left where this station writes.
//
// A volume that could not be interrogated answers nothing rather than zero: « 0 octet
// libre » because a syscall failed sends somebody deleting files (§15.4, control 5).
func (d *adminDashboard) disk() *web.DiskSpace {
	if d.dataDir == "" {
		return nil
	}
	space, err := d.machine.FreeSpace(d.dataDir)
	if err != nil || !space.Determined {
		return nil
	}
	return &web.DiskSpace{
		Path:      space.Path,
		FreeBytes: int64(space.FreeBytes), TotalBytes: int64(space.TotalBytes),
	}
}

// unattendedRestart reports bloquant-7, through the verdict `openscale doctor` gives at its
// third control — the same function, so the screen and doctor.txt cannot disagree.
func (d *adminDashboard) unattendedRestart(ctx context.Context) *web.RestartReadiness {
	d.once.Do(func() {
		control := diag.UnattendedRestartControl(ctx, d.machine)
		d.restart = web.RestartReadiness{
			Configured: control.Status == diag.StatusPass,
			Known:      control.Status != diag.StatusUnknown,
			Detail:     control.Observed,
			Remedy:     control.Remedy,
		}
	})
	readiness := d.restart
	return &readiness
}

// source reports what the catalog source in service is watching.
//
// The wording comes from the source ITSELF — « dépôt local, flv_2.csv dans … », « WebDAV,
// https://… (compte odoo) » — because it is the source that knows whether it has an
// account and which file name a station number derives (§10.1, §14.4).
func (d *adminDashboard) source() *web.CatalogSourceState {
	current := d.catalog.current()
	if current == nil {
		return nil
	}
	state := web.CatalogSourceState{Type: current.Name()}
	if described, ok := current.(interface{ Describe() string }); ok {
		state.Label = described.Describe()
	}
	return &state
}

// liveCatalog is the catalog source IN SERVICE, which a reload replaces too.
//
// The station swaps the source when catalog.* or station.number changes, and the file
// name it watches changes with it (flv_<n>.csv). An adapter holding the first source
// would drop a manually imported CSV into the directory of the previous station number.
type liveCatalog struct {
	mu     sync.RWMutex
	source ports.CatalogSource
}

// hold puts the source that has just taken service in place.
func (l *liveCatalog) hold(source ports.CatalogSource) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.source = source
}

// current reports the source in service, or nil when this station has none — which is an
// amber light and never a refusal to start (guiding principle 7).
func (l *liveCatalog) current() ports.CatalogSource {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.source
}
