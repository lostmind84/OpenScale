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

// adminTroubleshooting is the three unauthenticated switches of §14.4, and not one of
// them writes the configuration file — which is the criterion of ADR-018.
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
func (t adminTroubleshooting) ManualEntry(ctx context.Context, on bool) error {
	var asked domain.Config
	if !on {
		read, err := t.file.Read(ctx)
		if err != nil {
			return fmt.Errorf("la configuration de ce poste ne peut pas être relue, "+
				"la balance déclarée est donc inconnue : %w", err)
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
}

// hold puts the printer that has just taken service in place.
func (l *livePrinter) hold(printer ports.Printer) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.printer = printer
	l.wrapper, _ = printer.(*printing.Service)
}

// SelfTest prints one of the three built-in patterns of §8.6, on the printer in service.
//
// It satisfies web.SelfTester, which is one method wide on purpose: that is all the
// three self-test routes need, and a station whose printer could not be built still
// answers — with the reason the driver gives, which names the offending key.
func (l *livePrinter) SelfTest(ctx context.Context, what string) error {
	l.mu.RLock()
	printer := l.printer
	l.mu.RUnlock()
	if printer == nil {
		return errors.New("aucune imprimante n'est en service sur ce poste")
	}
	return printer.SelfTest(ctx, what)
}

// printService reports the print service, or says in French why there is none.
//
// A nil service is not a bug: a station whose printer.options name a queue this binary
// cannot open runs on the stand-in of serve.go, and the honest answer to « change the
// roll » there is that there is no printer to change it on.
func (l *livePrinter) printService() (*printing.Service, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.wrapper == nil {
		return nil, errors.New("l'imprimante de ce poste n'a pas pu être construite : " +
			"corrigez printer.type et printer.options dans les réglages avancés")
	}
	return l.wrapper, nil
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
