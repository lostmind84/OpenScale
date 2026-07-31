package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"openscale/internal/domain"
	"openscale/internal/station"
	"openscale/internal/station/ports"
	"openscale/internal/update"
)

// maxSubscribers is how many SSE streams one station serves at once (§13.1).
//
// Eight, and the ninth is REFUSED rather than queued. A station has one screen; the
// other seven are a volunteer's phone, a second browser tab and a curl left running
// overnight. Queuing the ninth would hold an HTTP goroutine for as long as somebody
// keeps the tab open, which is the leak §13.1 claims not to have.
const maxSubscribers = 8

// probeBudget is what /healthz gives the Hub to answer one event (§14.5).
//
// It is spent on the INJECTED clock: a liveness probe that reads the wall clock
// would make the shutdown test wait for real.
const probeBudget = 500 * time.Millisecond

// deviceBudget bounds the two troubleshooting actions that really do talk to a
// device — the self-test and the fallback switch.
//
// A handler NEVER waits on hardware without a deadline: the volunteer pressing the
// button is standing in front of the screen.
const deviceBudget = 10 * time.Second

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

// Options is everything the HTTP layer is given. Clock and Hub are required; every
// other collaborator is optional and its absence is answered honestly.
type Options struct {
	Clock ports.Clock
	Hub   Hub
	// Controller is the station. Without it the station cannot be reconfigured and
	// /healthz falls back to probing the Hub, which is what it measures anyway.
	Controller Controller
	// Technical is where a browser error and an administrative action are recorded.
	// station.Hub.TechnicalLog() is what a real station passes.
	Technical ports.TechnicalLog

	Store           Store
	Config          ConfigStore
	Catalog         CatalogAdmin
	Hardware        Hardware
	Printer         SelfTester
	Troubleshooting Troubleshooting
	// Diagnostic builds diagnostic.zip. Nil answers 501 on that route and nothing else:
	// a station that cannot produce an archive still weighs.
	Diagnostic Diagnostician
	// Dashboard feeds the roll light, the disk light, the catalog line and the unattended
	// restart indicator of §14.4. Nil leaves those four absent from GET /admin/api/health
	// and never fails it.
	Dashboard Dashboard
	// Update installs a newer release from the screen. Nil answers 501 on the act.
	Update Updater
	// Restart stops the station so that its supervisor starts it again. Nil answers
	// 501 on that route: see Restarter.
	Restart Restarter
	// Reboot restarts the machine, after the countdown of rebootPlan. Nil answers 501.
	Reboot Rebooter

	// Assets is the built front end (internal/web/dist through //go:embed). Nil
	// serves a placeholder page rather than a 404: a station whose front end has not
	// been built still has to say so on the screen it is standing in front of.
	Assets fs.FS
	// Images is the photo directory, rooted at <data>/images, laid out as
	// <2 first characters of the sha>/<sha>.<detected extension> (§10.7).
	Images fs.FS

	// Registries are what a configuration is validated against (§11.3): the drivers
	// and templates this binary actually carries.
	Registries domain.Registries
	// Binder owns the socket, and is what makes network.listen reloadable (§11.4,
	// ADR-027). Nil disables the rebind and nothing else.
	Binder *Binder
	// Version is what the dashboard displays next to the configuration fingerprint.
	Version string
}

// Server is the HTTP layer. It is built once and is safe for concurrent use.
type Server struct {
	clock           ports.Clock
	hub             Hub
	controller      Controller
	technical       ports.TechnicalLog
	store           Store
	configStore     ConfigStore
	catalog         CatalogAdmin
	hardware        Hardware
	printer         SelfTester
	troubleshooting Troubleshooting
	diagnostician   Diagnostician
	dashboard       Dashboard
	updater         Updater
	restarter       Restarter
	// rebootPlan is the countdown before the machine restarts, or nil on a platform
	// that cannot restart — which is what the two reboot routes answer 501 on.
	rebootPlan *rebootPlan
	assets     fs.FS
	images     fs.FS
	registries domain.Registries
	binder     *Binder
	version    string

	// subscribers counts the SSE streams in flight. An atomic and not a mutex: the
	// only question asked of it is « are there already eight? », and it is asked
	// from one handler goroutine per browser.
	subscribers atomic.Int64

	// catalogPayload caches the serialized catalog, keyed by the POINTER of the
	// snapshot it was built from. A catalog is immutable and only ever replaced
	// wholesale, so a pointer that has not moved describes bytes that cannot have.
	catalogPayload atomic.Pointer[catalogPayload]

	sessions *sessionStore
	handler  http.Handler
}

// New wires the HTTP layer over a running station.
func New(o Options) (*Server, error) {
	switch {
	case o.Clock == nil:
		return nil, errors.New("web: New: pas d'horloge ; tout budget se dépense sur l'horloge INJECTÉE (§5.3)")
	case o.Hub == nil:
		return nil, errors.New("web: New: pas de Hub ; la couche HTTP ne décide de rien elle-même")
	}
	if o.Technical == nil {
		o.Technical = ports.NopTechnicalLog{}
	}
	s := &Server{
		clock: o.Clock, hub: o.Hub, controller: o.Controller, technical: o.Technical,
		store: o.Store, configStore: o.Config, catalog: o.Catalog,
		hardware: o.Hardware, printer: o.Printer, troubleshooting: o.Troubleshooting,
		diagnostician: o.Diagnostic, dashboard: o.Dashboard, updater: o.Update,
		restarter: o.Restart,
		assets:    o.Assets, images: o.Images, registries: o.Registries,
		binder: o.Binder, version: o.Version,
		sessions: newSessionStore(o.Clock),
	}
	if o.Reboot != nil {
		s.rebootPlan = newRebootPlan(o.Clock, o.Reboot.Reboot, s.reportRebootRefused)
	}
	s.handler = s.routes()
	return s, nil
}

// Handler is the whole route table of §14.5.
func (s *Server) Handler() http.Handler { return s.handler }

// ServeHTTP lets a Server be used where an http.Handler is expected.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.handler.ServeHTTP(w, r) }

// HTTPServer builds the net/http server with the wiring §13.4 says not to forget.
//
// BaseContext is the point of this function. r.Context() derives from it, and it
// defaults to context.Background: without this line, cancelling the root context
// leaves every in-flight request context alive, Shutdown waits for SSE streams that
// never become idle, and the shutdown burns its entire budget every time a browser
// is connected — that is, always.
//
// RegisterOnShutdown is the second call site of CloseSubscribers, kept ON PURPOSE:
// it covers a Shutdown triggered without going through Station.Stop. It is safe
// because CloseSubscribers is idempotent (§13.3).
func (s *Server) HTTPServer(root context.Context, closeSubscribers func()) *http.Server {
	srv := &http.Server{
		Handler:     s.handler,
		BaseContext: func(net.Listener) context.Context { return root },
		// A header that never finishes arriving must not hold a connection for ever.
		// It is a network deadline like the write deadline of stream.go, and it is
		// the only budget of this file the injected clock does not own.
		ReadHeaderTimeout: 10 * time.Second,
	}
	if closeSubscribers != nil {
		srv.RegisterOnShutdown(closeSubscribers)
	}
	return srv
}

// routes is the table of §14.5, in the order the document writes it.
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	// --- The client screen -------------------------------------------------
	mux.HandleFunc("GET /", s.index)
	mux.HandleFunc("GET /admin", s.adminIndex)
	mux.HandleFunc("GET /admin/", s.adminIndex)
	mux.HandleFunc("GET /assets/", s.staticAsset)
	mux.HandleFunc("GET /images/{name}", s.image)

	mux.HandleFunc("GET /api/v1/stream", s.stream)
	mux.HandleFunc("GET /api/v1/catalog", s.catalogPage)
	mux.HandleFunc("POST /api/v1/weigh", s.weigh)
	mux.HandleFunc("POST /api/v1/reprint", s.reprint)
	mux.HandleFunc("POST /api/v1/cancel", s.cancel)
	mux.HandleFunc("POST /api/v1/dismiss", s.dismiss)
	mux.HandleFunc("POST /api/v1/ui/error", s.uiError)
	mux.HandleFunc("GET /api/v1/", notFound)

	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /readyz", s.readyz)

	// --- Open: everything one can LOOK AT, and the gestures that repair -----
	//
	// ADR-033 moved the criterion from the DOOR to the ACT: « ce qui change ce que le
	// poste vend, ou la façon dont il pèse » is protected, and the rest is not. Reading
	// a configuration is not one of those — `configPayload` redacts both hashes before
	// it leaves, so there is nothing here a password would be keeping.
	//
	// Making a volunteer type a password to LOOK at a port number, while whoever stands
	// behind the counter can already unplug the printer, bought nothing and cost the
	// whole of the troubleshooting.
	mux.HandleFunc("POST /admin/api/troubleshooting/reprint", s.troubleshootingReprint)
	mux.HandleFunc("POST /admin/api/troubleshooting/reload-catalog", s.reloadCatalog)
	mux.HandleFunc("POST /admin/api/troubleshooting/roll-changed", s.rollChanged)
	mux.HandleFunc("POST /admin/api/troubleshooting/fallback-printer", s.fallbackPrinter)
	mux.HandleFunc("POST /admin/api/troubleshooting/test-scale", s.testScale)
	mux.HandleFunc("POST /admin/api/troubleshooting/test-printer", s.testPrinter)
	mux.HandleFunc("POST /admin/api/troubleshooting/test-label", s.testLabel)
	mux.HandleFunc("GET /admin/api/diagnostic.zip", s.diagnostic)
	mux.HandleFunc("GET /admin/api/health", s.adminHealth)
	mux.HandleFunc("GET /admin/api/config", s.readConfig)
	mux.HandleFunc("GET /admin/api/config/versions", s.configVersions)
	mux.HandleFunc("GET /admin/api/ports", s.listPorts)
	mux.HandleFunc("GET /admin/api/printers", s.listPrinters)
	mux.HandleFunc("GET /admin/api/update", s.updateStatus)
	mux.HandleFunc("GET /admin/api/label/preview.png", s.labelPreview)
	// The journal is open, EXPORT INCLUDED: the page already shows the 200 weighings,
	// and diagnostic.zip — open — carries them too. A lock on the third door is not one.
	mux.HandleFunc("GET /admin/api/journal", s.journal)
	mux.HandleFunc("GET /admin/api/journal/export.csv", s.journalCSV)
	mux.HandleFunc("GET /admin/api/technical", s.technicalJournal)
	mux.HandleFunc("GET /admin/api/imports", s.imports)

	mux.HandleFunc("POST /admin/api/session", s.openSession)
	mux.HandleFunc("DELETE /admin/api/session", s.closeSession)
	mux.HandleFunc("POST /admin/api/session/recovery", s.recoverSession)

	// --- Protected: what changes what the station sells, or how it weighs ---
	//
	// `manual-entry` and `catalog/import` are here and were not: the first cuts the
	// scale out and lets the CUSTOMER type their own weight, the second replaces the
	// whole grid with a file somebody brought. Both leave their trace at the till, and
	// both were heavier than anything the password was guarding.
	//
	// `config/export` is here although it only reads: it is the one payload that still
	// carries the password hash (§11.5).
	guarded := map[string]http.HandlerFunc{
		"PUT /admin/api/config":                        s.writeConfig,
		"POST /admin/api/config/confirm":               s.confirmConfig,
		"GET /admin/api/config/export":                 s.exportConfig,
		"POST /admin/api/config/import":                s.importConfig,
		"POST /admin/api/config/restore":               s.restoreConfig,
		"POST /admin/api/config/reload":                s.reloadConfigFromDisk,
		"POST /admin/api/restart":                      s.restart,
		"POST /admin/api/reboot":                       s.armReboot,
		"DELETE /admin/api/reboot":                     s.cancelReboot,
		"POST /admin/api/troubleshooting/manual-entry": s.manualEntry,
		"POST /admin/api/catalog/import":               s.importCatalog,
		"POST /admin/api/printers/discover":            s.discoverPrinters,
		"POST /admin/api/scale/detect":                 s.detectScale,
		"POST /admin/api/scale/capture":                s.captureScale,
		"POST /admin/api/printer/test":                 s.printerTest,
		"POST /admin/api/catalog/reload":               s.reloadCatalog,
		"POST /admin/api/catalog/forget-quarantine":    s.forgetQuarantine,
		"POST /admin/api/products/{id}/decision":       s.productDecision,
		"POST /admin/api/replay":                       s.replay,
		"POST /admin/api/update/check":                 s.updateCheck,
		"POST /admin/api/update/apply":                 s.updateApply,
	}
	for pattern, handler := range guarded {
		mux.HandleFunc(pattern, s.authenticated(handler))
	}
	mux.HandleFunc("GET /admin/api/", notFound)

	return s.guard(mux)
}

// --- Answers ---------------------------------------------------------------

// writeJSON renders one body, and never lets a half-written one look like a whole.
//
// The body is marshalled BEFORE the status line goes out: a marshalling failure
// after WriteHeader would leave the client with a 200 and a truncated document,
// which is the one failure mode a screen cannot detect.
func writeJSON(w http.ResponseWriter, status int, body any) {
	raw, err := json.Marshal(body)
	if err != nil {
		http.Error(w, `{"message":"Réponse illisible."}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}

// problem is what every refusal of this layer looks like.
//
// Message is FRENCH and complete: it is read by a volunteer on the administration
// screen. Code is an ERR-xxx-nn when one is allocated, and empty otherwise — an
// invented code is worse than none, because somebody would look it up.
type problem struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	// Faults carries the configuration controls of §11.3, ALL of them at once.
	Faults []faultDTO `json:"faults,omitempty"`
}

// writeProblem renders one refusal.
func writeProblem(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, problem{Code: code, Message: message})
}

// notFound is the answer of an /api path nobody serves. It is JSON and not the
// front end: an API that answers a route with an HTML page teaches a front end to
// parse HTML.
func notFound(w http.ResponseWriter, _ *http.Request) {
	writeProblem(w, http.StatusNotFound, "", "Cette adresse n'existe pas.")
}

// unavailable answers a route whose collaborator this station was not given.
//
// 501 and not 404: the route EXISTS, it is in the contract of §14.5, and it is this
// binary's wiring that does not carry the capability yet. A 404 would send a
// volunteer looking for a typo.
func unavailable(w http.ResponseWriter, what string) {
	writeProblem(w, http.StatusNotImplemented, "",
		"Cette fonction n'est pas disponible sur ce poste : "+what+".")
}

// decodeJSON reads one request body, and refuses what it cannot understand.
//
// The body is BOUNDED: a command from the screen is a few hundred bytes, and an
// unbounded read is an unbounded allocation on a station with 4 GB of RAM.
func decodeJSON(w http.ResponseWriter, r *http.Request, into any) bool {
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		writeProblem(w, http.StatusBadRequest, "", "Requête illisible : "+err.Error())
		return false
	}
	return true
}

// maxBodyBytes bounds a JSON command body. A weigh command is under 200 bytes; a
// whole configuration, which travels on PUT /admin/api/config, is a few kilobytes.
const maxBodyBytes = 1 << 20
