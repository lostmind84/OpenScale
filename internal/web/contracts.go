// This file holds WHAT THE HTTP LAYER NEEDS FROM THE REST OF THE STATION, and
// nothing it provides.
//
// Every interface here is declared ON THE CONSUMER'S SIDE: the real components
// satisfy them as they stand, and a test drives the routes with doubles that start
// no goroutine and open no port. That is what makes this package testable without
// a station, and what keeps §5.2 true -- no arrow leaves the domain, and the ones
// that arrive here are named here.

package web

import (
	"context"
	"io"
	"openscale/internal/domain"
	"openscale/internal/station"
	"openscale/internal/update"
	"time"
)

// Hub is what the HTTP layer needs from the single decision-making goroutine.
//
// Declared HERE, on the consumer's side: *station.Hub satisfies it as it stands,
// and a test drives the routes with a double that never starts a goroutine.
type Hub interface {
	// State returns the last published snapshot, without blocking.
	State() station.Snapshot
	// Submit hands one command to the loop and waits for its answer, on ctx.
	Submit(ctx context.Context, ev domain.Event, key string) (domain.Ack, error)
	// Subscribe returns the snapshot channel of one subscriber and its unsubscribe.
	Subscribe() (<-chan station.Snapshot, func())
	// Config returns the configuration in force.
	Config() domain.Config
	// Catalog returns the catalog in service, or nil before the first one.
	Catalog() *domain.Catalog
	// CatalogUpdatedAt returns when that catalog entered service, or the zero time.
	CatalogUpdatedAt() time.Time
	// DowntimeGuard reports whether the station may be taken down, and says in French
	// why not when it may not.
	//
	// The rule belongs to the station and is asked, never deduced: an HTTP layer that
	// read a state to conclude « somebody is weighing » would hold a second copy of a
	// rule that already has an owner.
	DowntimeGuard() (bool, string)
}

// Controller is what the HTTP layer needs from the station AROUND the loop: the
// liveness of §14.5 and the hot reload of §11.4.
type Controller interface {
	// Alive reports that the Hub loop is publishing.
	Alive() bool
	// Reload publishes a new configuration and restarts what actually changed.
	//
	// The request carries the FILE as it was before the change, and not only the new
	// configuration, because a rollback has two documents to put back: the station goes
	// back to what it was running, the file to what it carried. On the one station §11.3
	// serves — a faulty file, the neutral profile in memory — those are not the same
	// document, and handing over only one wrote the factory profile onto the shop's file.
	Reload(req station.ReloadRequest) (station.ReloadOutcome, error)
	// Confirm accepts the configuration in force and stops the 60 s countdown.
	Confirm() error
	// PendingConfirmation reports the end of the countdown still running, or the zero time.
	PendingConfirmation() time.Time
}

// Store is the persistence as the administration screens read it.
//
// It is declared HERE and not imported: internal/web knows no database package
// (§5.2). cmd/openscale adapts *store.DB to it, which is a handful of lines and the
// price of the cut.
type Store interface {
	// Weighings returns one page of the journal, most recent first.
	Weighings(ctx context.Context, q JournalQuery) ([]domain.Weighing, error)
	// CountWeighings reports how many rows the journal holds.
	CountWeighings(ctx context.Context) (int, error)
	// TechnicalEntries returns one page of the technical journal.
	TechnicalEntries(ctx context.Context, q TechnicalQuery) ([]TechnicalLine, error)
	// Imports returns the history of catalog imports, most recent first.
	Imports(ctx context.Context, limit, offset int) ([]domain.Import, error)
	// LastAppliedImport returns the most recent import that PUT A CATALOG IN SERVICE.
	//
	// It is not the same question as the first row of Imports: 'unchanged', 'rejected'
	// and 'failed' are rows too, and none of them changed what the station serves. The
	// error is « this station has never applied one » and carries no sentinel: every
	// caller here treats it as an absence.
	LastAppliedImport(ctx context.Context) (domain.Import, error)
	// Findings returns what one import had to say about the rows it read.
	Findings(ctx context.Context, importID int64) ([]domain.Finding, error)
	// LocalDecisions returns the human judgements in force (§10.6).
	LocalDecisions(ctx context.Context) ([]domain.LocalDecision, error)
	// SaveDecision records one human judgement about one product.
	SaveDecision(ctx context.Context, d domain.LocalDecision) error
	// ClearDecision removes the judgement about one product.
	ClearDecision(ctx context.Context, productID string) error
	// Image returns the metadata of one photo, addressed by its content.
	Image(ctx context.Context, sha string) (domain.Image, error)
}

// ConfigStore is the configuration FILE, with its five rotating versions (§11.4).
type ConfigStore interface {
	// Read returns the configuration AS IT STANDS ON DISK, which is not always the one
	// in force.
	//
	// The difference is the whole reason this method is on the interface. A station that
	// started out of service runs the NEUTRAL PROFILE (§11.3) while the file keeps the
	// shop's settings and the faults that put it there; a station that fell back to
	// manual entry runs something else again (§11.4). What the expert pages edit, and
	// what a rescue writes back, has to be « ce que l'exploitant a demandé » — otherwise
	// the first save replaces the tariffs, the safeguards and the categories of a
	// cooperative with the factory ones.
	Read(ctx context.Context) (domain.Config, error)
	// Save rotates the versions and writes atomically: tmp, fsync, rename.
	Save(ctx context.Context, cfg domain.Config) error
	// Versions lists the restorable versions, most recent first.
	Versions(ctx context.Context) ([]ConfigVersion, error)
	// Restore reads back one version WITHOUT applying it.
	Restore(ctx context.Context, version int) (domain.Config, error)
}

// CatalogAdmin is the catalog source as the administration screen acts on it.
type CatalogAdmin interface {
	// Reload asks the source for a fresh batch now, and reports IN FRENCH what it saw of
	// the file it watches.
	//
	// That sentence is the ONE fact the watch never produces: it polls, finds nothing and
	// returns in silence, so « Recharger le catalogue » used to be followed by nothing at
	// all. It is EMPTY when the source watches no file of this machine — a share is
	// watched over the network — because an absence nobody checked must not be asserted.
	Reload(ctx context.Context) (string, error)
	// Import takes a CSV dropped on the screen and writes it where the ordinary
	// watcher will find it — same parser, same qualification (A4, ADR-011).
	Import(ctx context.Context, name string, r io.Reader) (domain.Import, error)
	// ForgetQuarantine clears the memory of the files that were refused.
	ForgetQuarantine(ctx context.Context) error
}

// Hardware answers the « what is actually plugged in? » questions of the expert
// screens (§14.4). Every method is platform-specific, which is why none of them
// lives here.
type Hardware interface {
	// Ports enumerates the serial ports, with their USB description.
	Ports(ctx context.Context) ([]PortInfo, error)
	// Printers enumerates the print queues the platform knows about.
	Printers(ctx context.Context) ([]PrinterInfo, error)
	// DiscoverPrinters looks for a label printer beyond the declared queues.
	DiscoverPrinters(ctx context.Context) ([]PrinterInfo, error)
	// DetectScale opens one port, applies the parsers and says what answered.
	DetectScale(ctx context.Context, port string) (ScaleDetection, error)
	// CaptureFrames records raw frames from one port for a bounded duration.
	CaptureFrames(ctx context.Context, port string, d time.Duration) ([]string, error)
	// LabelPreview renders the label as a PNG, identical to what would print (A2).
	LabelPreview(ctx context.Context, q PreviewQuery) ([]byte, error)
	// Replay pushes one recorded frame back through the decoder (§14.4, Journal).
	Replay(ctx context.Context, frame string) error
}

// Diagnostician writes diagnostic.zip (§15.4).
//
// It is its OWN interface and not a method of Hardware, for two reasons that both matter.
// The archive is not a platform question — internal/diag builds it out of the configuration,
// the journal and the fifteen controls — and its route is the one route of this group that
// carries NO password: §15.4 gives it « un seul bouton, sans mot de passe » because it is the
// only realistic remote support mechanism for a team of volunteers. Grouping it with the
// expert hardware calls would make one nil collaborator disable both.
type Diagnostician interface {
	// Diagnostic writes the archive into w. It never returns before the archive is complete
	// or the reason it is not is recorded inside it.
	Diagnostic(ctx context.Context, w io.Writer) error
}

// Dashboard answers the four questions of §14.4 that no HTTP layer can put to itself:
// how far the roll has gone, how much room is left on the disk, whether this machine
// comes back on its own after a power cut, and what the catalog source is watching.
//
// It is one collaborator and not four because it has one caller — the dashboard route —
// and because the composition root holds all four in the same hand: the print service,
// the data directory, the platform and the source in service. Nil leaves the four facts
// out of the payload, and the screen SAYS what it cannot see.
type Dashboard interface {
	// Dashboard reports what it could establish. Every field is optional and its absence
	// is the honest answer: nothing here is worth a 500 on the one page a volunteer opens
	// when the station is already broken.
	Dashboard(ctx context.Context) DashboardFacts
}

// DashboardFacts is what only the composition root knows.
type DashboardFacts struct {
	Roll    *RollGauge
	Disk    *DiskSpace
	Restart *RestartReadiness
	Source  *CatalogSourceState
	Routing *PrintRouting
}

// PrintRouting is which printer the labels are coming out of.
//
// Available is what decides whether the troubleshooting page offers « Imprimer sur
// l'imprimante du poste N » (§14.4) — a button offered on a station with no fallback
// configured would be a button that answers 501 to somebody already in trouble.
type PrintRouting struct {
	Available  bool
	OnFallback bool
	// Name is the FRENCH name of the printer in use, and Banner the permanent line of
	// §8.4 — both come from the print service, which is where the wording belongs.
	Name   string
	Banner string
}

// RollGauge is the label counter of §8.5 as the « rouleau » light reads it.
//
// The wording travels WITH the numbers, because it is printing.RollCounter that knows
// when « environ 100 étiquettes restantes » becomes « le rouleau est probablement fini »,
// and a screen that recomputed the sentence from the numbers would be a second opinion on
// a threshold that already has an owner.
type RollGauge struct {
	Printed  int64
	Capacity int
	// Remaining CAN be negative: a roll that held more than the configured capacity, or
	// one changed without anybody saying so, which is the ordinary case (§8.5).
	Remaining int64
	Level     string
	Message   string
	Known     bool
}

// DiskSpace is the room left on the volume the station writes to.
type DiskSpace struct {
	Path       string
	FreeBytes  int64
	TotalBytes int64
}

// RestartReadiness is bloquant-7: after a power cut, does this station come back to the
// client screen without anybody typing a Windows password?
type RestartReadiness struct {
	Configured bool
	// Known is false when the question could not be put to the system at all.
	Known  bool
	Detail string
	Remedy string
}

// CatalogSourceState is the permanent catalog line of §14.4: the source, the path or the
// URL watched, and the account used.
type CatalogSourceState struct {
	Type  string
	Label string
}

// SelfTester prints one of the three built-in patterns (§8.6). ports.Printer
// satisfies it, and that is the only reason it is one method wide.
type SelfTester interface {
	// SelfTest prints "label", "alignment" or "ruler".
	SelfTest(ctx context.Context, what string) error
}

// Troubleshooting is what the repair buttons of §14.4 act on and that nothing else in
// this package can reach.
//
// None of the three writes the configuration file: manual entry is a STATE the station
// enters, the roll counter is a counter, and the fallback printer is a route for the
// current session. That was the criterion of ADR-018, and it is no longer the one that
// decides the door — ADR-033 asks what an act CHANGES. Two of the three stay open, and
// ManualEntry is authenticated: it cuts the scale out and lets the customer type their own
// weight. The route table below is where that is settled, not this interface.
type Troubleshooting interface {
	// ManualEntry switches the station into, or out of, manual weight entry.
	ManualEntry(ctx context.Context, on bool) error
	// RollChanged resets the label counter of the roll (§8.5).
	RollChanged(ctx context.Context) error
	// UseFallbackPrinter routes printing to the neighbouring station's printer.
	UseFallbackPrinter(ctx context.Context, on bool) error
}

// Updater is what the HTTP layer needs to move the station to a newer release.
//
// Declared here, on the consumer's side; *update.Service satisfies it. Nil answers
// 501 on the act and « not supported » on the read, which is what a Linux station
// honestly is: hiding the routes would leave a screen guessing, and a button doing
// nothing would be worse than none.
type Updater interface {
	// Status answers the screen from what is on disk, without polling.
	Status(repository string) (update.Status, error)
	// Check polls the repository now and records what it found.
	Check(ctx context.Context, repository string) (update.Check, error)
	// Apply brings the wanted version down and hands the swap over. It returns
	// as soon as the swap has STARTED: what finishes it also stops this process.
	Apply(ctx context.Context, repository, wanted string) error
}

// Restarter stops the station so that its supervisor starts it again.
//
// Declared here, on the consumer's side; *stationRestarter of cmd/openscale satisfies
// it. NIL MEANS « nobody would relaunch it », and the route then answers 501 instead of
// stopping a station that would stay down — which is what `openscale serve` typed into
// a terminal is.
//
// This is the route ADR-027 removed, and it is not that route. What the ADR refuses is
// a restart DEMANDED BY A SETTING: no configuration block may ask for one, and none
// does. This one is a repair, and it goes through the only restart that ADR calls
// legitimate — the one the SCM or systemd triggers on its own.
type Restarter interface {
	// Restart asks the station to stop. It returns as soon as the demand is recorded,
	// because what carries it out also ends this process, and a *station.DowntimeRefused
	// when the station must not be taken down right now.
	Restart() error
}

// Rebooter restarts THE MACHINE.
//
// Declared here, on the consumer's side; platform.Reboot satisfies it once adapted. Nil
// answers 501: a station whose platform cannot restart must say so rather than offer a
// button that fails at the last click, and « ce poste ne sait pas faire » is a different
// piece of news from « ça n'a pas marché ».
type Rebooter interface {
	// Reboot restarts the machine. It returns as soon as the demand is accepted.
	Reboot() error
}
