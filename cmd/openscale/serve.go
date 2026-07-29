package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	catalogpkg "openscale/internal/catalog"
	"openscale/internal/catalog/importer"
	"openscale/internal/diag"
	"openscale/internal/domain"
	"openscale/internal/platform"
	"openscale/internal/printing"
	"openscale/internal/scale"
	"openscale/internal/scale/absent"
	"openscale/internal/station"
	"openscale/internal/station/ports"
	"openscale/internal/store"
	"openscale/internal/web"
)

// THIS FILE IS THE COMPOSITION ROOT (§3, §5.2).
//
// It is the only place where a configuration file, a database, two driver registries,
// the Hub and an HTTP server meet. Everything below it is injected: the station knows
// no driver, the web layer knows no database, and no package but this one and
// drivers.go can name a serial port or a print queue.
//
// The shutdown is NOT re-implemented here. Station.Stop carries the order of §13.4 —
// cancel the root, wait for the loop to RETURN, then close the subscribers — and this
// file's only contribution is to cancel the root context BEFORE calling it, so that
// every in-flight request context dies with it (the HTTP server derives them from that
// context through BaseContext).

// The exit codes a service manager reads.
const (
	// exitFailure is what any refusal of a subcommand returns: a configuration that
	// cannot be read, a database that cannot be opened. systemd restarts, the Windows
	// SCM restarts, and the install sheet says what to look at.
	exitFailure = 1
	// exitFatal is the code of §13.4: the socket could not be taken, or the server
	// stopped serving on its own. « Un poste ne peut pas tourner normalement en étant
	// mort. »
	exitFatal = 3
)

// The technical codes of §13.4, each with the sentence a volunteer reads.
const (
	// codeAnotherInstance is ERR-SYS-01: the address refuses a bind AND answers a
	// probe. THE SOCKET IS THE SINGLE-INSTANCE LOCK — no lock file left behind by a
	// crash, no Windows named mutex — and telling this case from the next one is what
	// keeps a volunteer from hunting for a ghost process.
	codeAnotherInstance = "ERR-SYS-01"
	// codeCannotListen is ERR-SYS-02: the address refuses a bind and answers nothing.
	// It is an address this station cannot have, which is a different remedy.
	codeCannotListen = "ERR-SYS-02"
	// codeServerStopped is ERR-SYS-03: Serve returned without a shutdown having been
	// asked for.
	codeServerStopped = "ERR-SYS-03"
)

// probeBudget is how long the single-instance probe waits for the address to answer.
//
// It is a NETWORK deadline in the TCP stack of the kernel, of the same nature as the
// write deadline of internal/web/stream.go, and it is spent before the injected clock
// exists as far as this decision is concerned: no business decision rests on it, and no
// test can be made to wait on it — a refused bind answers or does not answer at once.
const probeBudget = 250 * time.Millisecond

// serviceFailure is a failure of `openscale serve` that names its technical code and
// the exit code the service manager reads.
//
// §13.4 has `fatal` write to the text journal, to the technical journal AND to stderr,
// then exit 3. Those are three different call sites here, deliberately: the technical
// journal is written where the failure happens, because only there is the database in
// hand; stderr and the exit code belong to main, because only main can exit.
type serviceFailure struct {
	// Code is the ERR-SYS-nn a volunteer reads on the telephone.
	Code string
	// Exit is what the process returns.
	Exit int
	// Message is FRENCH and complete: it names what is wrong and what to do about it.
	Message string
	// Err is the underlying failure, kept so that errors.Is reaches it.
	Err error
}

// Error reports the code and the French sentence, which is what stderr carries.
func (f *serviceFailure) Error() string {
	if f.Code == "" {
		return f.Message
	}
	return f.Code + " : " + f.Message
}

// Unwrap yields the failure this one was built on.
func (f *serviceFailure) Unwrap() error { return f.Err }

// exitCodeFor reports the code the process returns for one error.
func exitCodeFor(err error) int {
	var failure *serviceFailure
	if errors.As(err, &failure) && failure.Exit != 0 {
		return failure.Exit
	}
	return exitFailure
}

// serveOptions is what `openscale serve` was told, once the flags, the two environment
// variables of §11.1 and the defaults have been resolved.
type serveOptions struct {
	// configPath is the file the station reads its configuration from.
	configPath string
	// dataDir holds the database, the images and the captured labels.
	dataDir string
	// listen overrides network.listen, and empty means « the address the file names ».
	listen string

	// serving is called once the socket is open and the routes are served, with the
	// address the station really listens on. It is nil in production: it is what a test
	// waits on instead of polling a port, and it is the only reason this struct is not
	// three strings.
	serving func(address string)
}

// runServe is the subcommand of §15.1: the service itself, and the default of the
// binary.
//
// It returns when ctx is done — a SIGTERM from systemd, a stop from the Windows SCM —
// or when the HTTP server stopped serving on its own, which is ERR-SYS-03.
func runServe(ctx context.Context, args []string, out io.Writer) error {
	o, err := parseServeOptions(args, out)
	if err != nil {
		return err
	}
	return serve(ctx, o, out)
}

// parseServeOptions resolves the three flags of `serve` against the environment and
// the defaults of §11.1.
//
// The precedence is flag, then environment, then default, and it is the usual one for
// a reason that is not habit: the flag is what a person types once while diagnosing,
// the variable is what the service unit carries, and the default is what an
// installation that touched neither gets.
func parseServeOptions(args []string, out io.Writer) (serveOptions, error) {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(out)
	var (
		configPath = fs.String("config", os.Getenv("OPENSCALE_CONFIG"), "fichier de configuration")
		dataDir    = fs.String("data", os.Getenv("OPENSCALE_DATA"), "répertoire de données")
		listen     = fs.String("listen", "", "adresse d'écoute, sinon celle de la configuration")
	)
	fs.Usage = func() {
		fmt.Fprint(out, `Usage : openscale serve [--config fichier] [--data répertoire] [--listen hôte:port]

Le service du poste de pesée : la balance, l'imprimante, le catalogue, la base et
l'écran client. C'est ce que lance le service Windows ou l'unité systemd.

Options :
  --config <fichier>       configuration du poste ; sinon OPENSCALE_CONFIG, sinon
                           l'emplacement par défaut du système
  --data <répertoire>      base, images et étiquettes ; sinon OPENSCALE_DATA, sinon
                           l'emplacement par défaut du système
  --listen <hôte:port>     adresse d'écoute ; sinon celle du fichier de configuration
`)
	}
	positional, err := parseMixed(fs, args)
	if err != nil {
		return serveOptions{}, err
	}
	if len(positional) != 0 {
		fs.Usage()
		return serveOptions{}, fmt.Errorf("argument inattendu %q : serve ne prend que des options", positional[0])
	}

	o := serveOptions{configPath: *configPath, dataDir: *dataDir, listen: *listen}
	if o.configPath == "" {
		o.configPath = platform.DefaultConfigPath()
	}
	if o.dataDir == "" {
		o.dataDir = platform.DefaultDataDir()
	}
	return o, nil
}

// serve wires the station and runs it until ctx is done.
func serve(ctx context.Context, o serveOptions, out io.Writer) error {
	clock := platform.NewSystemClock()

	cfg, err := readConfig(o.configPath)
	if err != nil {
		return err
	}
	if o.listen != "" {
		cfg.Network.Listen = o.listen
	}
	// The service OWNS its data directory and creates it (§15.3). Nothing here is a
	// mount point and nothing here is shared: an administrator who wants files to
	// arrive from a share mounts what they like where they like and synchronises into
	// it, and the unit has nothing to know about it.
	for _, dir := range []string{o.dataDir, imagesRoot(o.dataDir), labelsDir(o.dataDir)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return &serviceFailure{Exit: exitFailure, Err: err, Message: fmt.Sprintf(
				"le répertoire de données %s ne peut pas être créé : %v", dir, err)}
		}
	}

	db, err := store.Open(platform.DatabasePath(o.dataDir), clock)
	if err != nil {
		return &serviceFailure{Exit: exitFailure, Err: err, Message: fmt.Sprintf(
			"la base %s ne peut pas être ouverte : %v", platform.DatabasePath(o.dataDir), err)}
	}
	// The base is closed exactly ONCE, by whoever owns it at that moment. Until the
	// station exists this function does; afterwards Station.Stop closes it LAST, after
	// the workers have been drained, which is the order of §13.4 — and a second Close
	// here would close what the shutdown has just checkpointed.
	stationOwnsTheBase := false
	defer func() {
		if !stationOwnsTheBase {
			_ = db.Close()
		}
	}()

	// The configuration FILE, with its five rotating versions and its atomic write
	// (§11.4). It is what makes the administration screen able to save, and what tells
	// « ce que l'exploitant a demandé » from what the station is actually doing after a
	// fallback to manual entry.
	configFile, err := platform.NewConfigStore(o.configPath)
	if err != nil {
		return &serviceFailure{Exit: exitFailure, Err: err, Message: fmt.Sprintf(
			"la configuration %s ne peut pas être gérée : %v", o.configPath, err)}
	}

	scales, printers := scaleRegistry(), printerRegistry()
	// The catalog sources belong in there too: without them control 9 cannot check
	// catalog.type, and PUT /admin/api/config would accept a source no station can open —
	// an amber light and an empty grid instead of a fault next to the field.
	registries := registries()
	// The probe of controls 44 and 46 answers FROM THE CONTEXT OF THE SERVICE ACCOUNT, so
	// only a process that is that account may carry one — `openscale config validate` on a
	// volunteer's laptop leaves it nil and validates the form, not the existence. It is
	// wired here rather than inside registries() for that reason, and because the archive
	// directory it must recognise is a fact about THIS station's data directory.
	registries.Paths = platform.NewPathChecker(o.dataDir)

	// §11.3: an invalid configuration NEVER kills the process. The station starts on
	// the neutral profile, IN MEMORY AND WITHOUT WRITING, in the one terminal state,
	// and the administration screen serves the whole list of faults. A broken
	// configuration must never produce a black screen.
	faults := cfg.Validate(registries)
	outOfService := len(faults) > 0
	if outOfService {
		reportFaults(out, o.configPath, faults)
		cfg = fallbackProfile(cfg, o.listen)
	}

	// The technical journal belongs to the Hub, and the drivers are built BEFORE it
	// exists. The relay is what carries the lines of that interval, on stderr, where
	// whoever started the service by hand can see them.
	log := &relayLog{fallback: out}

	templates, err := templatesFor(cfg, registries)
	if err != nil {
		return err
	}
	// The two holders of admin.go: a reload REPLACES the printer and the catalog source,
	// and the administration screens act on the one IN SERVICE, never on the one this
	// process started with (§11.4).
	live, liveSource := &livePrinter{}, &liveCatalog{}

	printer := buildPrinter(cfg, printers, templates, clock, log, o.dataDir)
	live.hold(printer)
	weigher := buildScale(cfg, scales, clock, log)

	catalog, err := db.LoadCatalog(ctx)
	if err != nil {
		return &serviceFailure{Exit: exitFailure, Err: err, Message: fmt.Sprintf(
			"le catalogue en base ne peut pas être lu : %v", err)}
	}
	if catalog != nil && catalog.Len() == 0 {
		// A station whose store has never received a catalog is INITIALIZING and says
		// « Catalogue vide. En attente de flv_<n>.csv » (§15.4), rather than Idle in
		// front of an empty grid that looks like a catalog with nothing in it.
		catalog = nil
	}

	// The catalog is the last thing wired, and the only one whose absence is not a
	// refusal to start: a station with no source still weighs what it already knows.
	images, err := catalogpkg.NewImageStore(o.dataDir)
	if err != nil {
		return &serviceFailure{Exit: exitFailure, Err: err, Message: fmt.Sprintf(
			"le répertoire des images ne peut pas être préparé : %v", err)}
	}
	applier, err := importer.New(importer.Options{Records: db, Clock: clock, Log: log})
	if err != nil {
		return &serviceFailure{Exit: exitFailure, Err: err, Message: fmt.Sprintf(
			"l'import du catalogue ne peut pas être préparé : %v", err)}
	}
	newCatalog := func(next domain.Config) (ports.CatalogSource, error) {
		return newCatalogSource(next, o.dataDir, clock, log, images, db)
	}
	// A source that cannot be built is an AMBER light, never a station that refuses to
	// start (guiding principle 7): the catalog already in the base keeps serving, and
	// the reason is journalled where a volunteer will read it.
	source, err := newCatalog(cfg)
	if err != nil {
		log.Technical(domain.LevelWarn, "catalog", "ERR-CAT-01",
			"La source du catalogue n'a pas pu être ouverte.", err.Error())
		source = nil
	}
	liveSource.hold(source)

	rootCtx, cancelRoot := context.WithCancel(ctx)
	defer cancelRoot()

	// The HTTP server needs the Hub to close its subscribers, and the station needs the
	// server to shut it down: two objects that each want the other first. The holder
	// breaks the knot without moving the shutdown out of Station.Stop, which is where
	// §13.4 lives.
	httpHolder := &heldServer{}

	// The update service needs the Hub to ask « may the station be taken down? »,
	// and the station needs the service to run its daily poll: the same knot as
	// the one above, untied the same way. The closure reads `st` when a volunteer
	// touches a button, which is long after the line that assigns it.
	var st *station.Station
	updateService := newUpdateService(clock,
		guardFunc(func() (bool, string) { return st.Hub().UpdateGuard() }), o.dataDir)

	st, err = station.New(station.Options{
		Clock:        clock,
		Config:       cfg,
		Catalog:      catalog,
		OutOfService: outOfService,
		// The same registries that decided the station was out of service decide when it
		// stops being: the answer must not depend on which of the two moments asked.
		Registries:    registries,
		Templates:     templates,
		Poller:        newUpdatePoller(updateService),
		Scale:         weigher,
		Printer:       printer,
		Journal:       db,
		TechnicalSink: technicalSink{db},
		NewScale:      func(next domain.Config) (ports.Scale, error) { return newScale(next, scales, clock, log) },
		// The two factories HOLD what they built, and only on success: the station keeps
		// the printer that works when a new one cannot be built (§11.4), so a holder
		// filled before that decision would name a printer nobody prints on.
		NewPrinter: func(next domain.Config) (ports.Printer, error) {
			rebuilt, err := newPrinter(next, printers, templates, clock, log, o.dataDir)
			if err != nil {
				return nil, err
			}
			live.hold(rebuilt)
			return rebuilt, nil
		},
		CatalogSource: source,
		NewCatalogSource: func(next domain.Config) (ports.CatalogSource, error) {
			rebuilt, err := newCatalog(next)
			if err != nil {
				return nil, err
			}
			liveSource.hold(rebuilt)
			return rebuilt, nil
		},
		ApplyCatalog: applier.Apply,
		Server:       httpHolder,
		Store:        db,
		// The rollback of §11.4 puts the FILE back as well as the running station: a
		// station that went back on its own and then restarted on the configuration nobody
		// confirmed would have cut the branch sixty seconds later than announced.
		OnRevert: func(previous domain.Config) {
			if err := configFile.Save(context.Background(), previous); err != nil {
				log.Technical(domain.LevelError, "config", "",
					"Retour arrière appliqué au poste mais non écrit : le fichier porte "+
						"encore la configuration non confirmée.", err.Error())
				return
			}
			log.Technical(domain.LevelWarn, "config", "",
				"Configuration non confirmée : le fichier est revenu à la version précédente.",
				previous.Fingerprint())
		},
	})
	if err != nil {
		return &serviceFailure{Exit: exitFailure, Err: err, Message: "le poste n'a pas pu être construit : " + err.Error()}
	}
	// From here the STATION owns the base, the printer and the scale, and Stop releases
	// all three. It is idempotent, so the nominal path calls it explicitly below and
	// this one covers the refusals in between — a socket already taken must not leave a
	// serial port open and a database unchecked-pointed behind it.
	stationOwnsTheBase = true
	defer st.Stop()
	log.attach(st.Hub().TechnicalLog())

	binder, err := listen(clock, cfg.Network.Listen, st.Hub().TechnicalLog())
	if err != nil {
		recordFailure(db, clock, err)
		return err
	}

	journal := adminStore{db}
	// The archive of §15.4, wired even when it cannot be prepared: a station that refused to
	// start because it could not get a SUPPORT file ready would be the joke version of
	// guiding principle 7. A nil diagnostician answers 501 on that one route and nothing else.
	diagnostic, err := newStationDiagnostic(o, clock, binder.Addr().String(), registries, db)
	if err != nil {
		st.Hub().TechnicalLog().Technical(domain.LevelWarn, "system", "",
			"Le fichier de diagnostic n'a pas pu être préparé : le bouton « Télécharger le "+
				"fichier de diagnostic » répondra que la fonction n'est pas disponible sur ce poste.",
			err.Error())
		diagnostic = nil
	}

	server, err := web.New(web.Options{
		Clock:      clock,
		Hub:        st.Hub(),
		Controller: st,
		Technical:  st.Hub().TechnicalLog(),
		Assets:     web.Assets(),
		Images:     imagesDir(o.dataDir),
		Registries: registries,
		Binder:     binder,
		Version:    version,

		// THE ADMINISTRATION ROUTES, wired to the real thing (§14.5). Every one of these
		// collaborators is an interface internal/web declares for itself: the HTTP layer
		// names no database, no serial port and no print queue, and this is the one place
		// where the two halves meet (cut 3 of §5.2).
		Store:  journal,
		Config: adminConfig{configFile},
		Catalog: adminCatalog{
			source: liveSource, db: db, clock: clock,
			log: st.Hub().TechnicalLog(), config: st.Hub().Config,
		},
		Hardware: adminHardware{
			clock: clock, hub: st.Hub(), registries: registries,
			technical: st.Hub().TechnicalLog(),
		},
		Printer:         live,
		Troubleshooting: adminTroubleshooting{station: st, printer: live, file: configFile},
		// diagnostic.zip is its OWN collaborator and not part of Hardware: it is not a
		// platform question, and its route is the one route of that group with no password
		// (§15.4, ADR-018).
		Diagnostic: diagnostic,
		// The four facts of the dashboard that live nowhere else: the roll counter belongs
		// to the print service, the free space and the registry to the platform, and the
		// watched path to the source in service (§14.4).
		Dashboard: &adminDashboard{
			printer: live, catalog: liveSource,
			machine: diag.NewMachine(clock), dataDir: o.dataDir,
		},
		Update: updateService,
	})
	if err != nil {
		_ = binder.Close()
		return &serviceFailure{Exit: exitFailure, Err: err, Message: "la couche HTTP n'a pas pu être construite : " + err.Error()}
	}

	// BaseContext is what §13.4 says not to forget: r.Context() derives from it, and
	// without it Shutdown waits for SSE streams that never become idle.
	httpServer := server.HTTPServer(rootCtx, st.Hub().CloseSubscribers)
	// Station.Stop is what shuts this server down, in the order of §13.4.
	httpHolder.hold(httpServer)

	returned := make(chan error, 1)
	go func() { returned <- st.Start(rootCtx) }()
	<-st.Ready()

	served := make(chan error, 1)
	go func() { served <- httpServer.Serve(binder) }()

	fmt.Fprintf(out, "openscale %s — poste en écoute sur http://%s\n", version, binder.Addr())
	if o.serving != nil {
		o.serving(binder.Addr().String())
	}

	var fatal error
	select {
	case <-ctx.Done():
	case err := <-served:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			fatal = &serviceFailure{Code: codeServerStopped, Exit: exitFatal, Err: err, Message: fmt.Sprintf(
				"le poste a cessé de servir sur %s : %v. Un poste ne peut pas tourner normalement en étant mort.",
				cfg.Network.Listen, err)}
			recordFailure(db, clock, fatal)
		}
	}

	// THE ORDER IS THE FIX (§13.4). Cancelling the root here, before Stop, is what ends
	// every in-flight request context; Stop then waits for the loop to RETURN and only
	// after that closes the subscriber channels, which is what lets Shutdown find no
	// active stream.
	cancelRoot()
	st.Stop()
	<-st.Stopped()
	<-returned
	fmt.Fprintf(out, "openscale : arrêt terminé en %s\n", st.StopDuration())
	return fatal
}

// listen opens the socket and tells the TWO failures apart.
//
// THE SOCKET IS THE SINGLE-INSTANCE LOCK (internal/web/binder.go), and that package
// deliberately leaves the discrimination to its caller: only the caller can probe the
// address. The two cases need two different sentences — an address that refuses a bind
// AND answers is another instance of this very application (ERR-SYS-01); one that
// refuses and answers nothing is an address this station cannot have (ERR-SYS-02) —
// and sending a volunteer hunting for a ghost process is the failure this tells apart.
func listen(clk ports.Clock, address string, log ports.TechnicalLog) (*web.Binder, error) {
	binder, err := web.Listen(clk, address, log)
	if err == nil {
		return binder, nil
	}
	if respondsToProbe(address) {
		return nil, &serviceFailure{Code: codeAnotherInstance, Exit: exitFatal, Err: err, Message: fmt.Sprintf(
			"une autre instance d'OpenScale est déjà lancée sur ce poste : %s répond déjà. "+
				"Arrêtez le service avant d'en lancer un second.", address)}
	}
	return nil, &serviceFailure{Code: codeCannotListen, Exit: exitFatal, Err: err, Message: fmt.Sprintf(
		"impossible d'écouter sur %s : %v. Cette adresse n'appartient pas à ce poste, "+
			"ou le port est réservé.", address, err)}
}

// respondsToProbe reports whether something is already answering on that address.
//
// A bare TCP connection and nothing more. Asking /healthz would say « and it is us »,
// which is a stronger claim than this decision needs and a weaker probe than it looks:
// an instance in « configuration d'usine » answers, one wedged mid-shutdown may not,
// and either way the remedy a volunteer reads is the same — stop what is holding the
// address before starting a second one.
func respondsToProbe(address string) bool {
	conn, err := net.DialTimeout("tcp", address, probeBudget)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// recordFailure writes one fatal error to the technical journal, which is the half of
// §13.4's `fatal` that survives the process.
//
// It is written SYNCHRONOUSLY and directly to the store: the Hub's journal worker may
// not be running yet, or may already have been drained. And it is written on a FRESH
// context, never on the one that is being cancelled — the line that says why the
// station is stopping must not be the first casualty of the stop.
func recordFailure(db *store.DB, clk ports.Clock, err error) {
	var failure *serviceFailure
	if !errors.As(err, &failure) {
		return
	}
	_ = db.RecordTechnical(context.Background(), store.TechnicalEntry{
		OccurredAt: clk.Now(), Level: store.LevelCritical, Source: store.LogSourceSystem,
		Code: failure.Code, Message: failure.Message, Detail: detailOf(failure.Err),
	})
}

// detailOf reports the technical tail of a failure, or nothing.
func detailOf(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// readConfig reads config.json.
//
// A file that cannot be READ is not the « configuration invalide » of §11.3, and the
// difference decides whether the process starts. An invalid configuration is one we
// UNDERSTOOD and can list the faults of, field by field, on a screen a volunteer then
// fixes; a file that does not parse yields one parse error at one byte offset, no
// station number, no listening address, and nothing the administration screen could
// safely write back — saving from that screen would overwrite a file whose content was
// never understood. So this one refuses, in French, naming the file, and the service
// manager restarts the way it does for any other failed start.
func readConfig(path string) (domain.Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return domain.Config{}, &serviceFailure{Exit: exitFailure, Err: err, Message: fmt.Sprintf(
			"le fichier de configuration %s ne peut pas être lu : %v", path, err)}
	}
	var cfg domain.Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return domain.Config{}, &serviceFailure{Exit: exitFailure, Err: err, Message: fmt.Sprintf(
			"le fichier de configuration %s n'est pas un JSON exploitable : %v", path, err)}
	}
	return cfg, nil
}

// fallbackProfile is what a station RUNS when its own configuration is unusable (§11.3).
//
// It is the neutral profile, plus the two things that must survive the fallback — and
// both were found by starting a station out of the box and trying to repair it from its
// own screen.
//
// # The administration block
//
// §11.3 replaces the configuration a station OPERATES ON. It has no business replacing
// the identity of whoever administers it: the password and the recovery code are the
// answer to « qui a le droit de réparer ce poste », and that answer is on the
// installation sheet, in the shop's folder, matching the hash IN THE FILE. Dropping them
// left the login form answering « aucun mot de passe n'est défini » and the recovery form
// answering « ce poste n'a pas de code de secours » — on the ONE station both exist for.
// The screen was then unreachable on exactly the station §11.3 says it must serve.
//
// # The listening address
//
// --listen is what somebody types while diagnosing — « the address is taken, move this
// station off it ». The neutral profile carries 127.0.0.1:8085 like every station of the
// parc, so dropping the override answered a deliberate instruction with the very address
// the operator was trying to leave.
func fallbackProfile(broken domain.Config, listen string) domain.Config {
	cfg := domain.NeutralProfile()
	cfg.Admin = broken.Admin
	if listen != "" {
		cfg.Network.Listen = listen
	}
	return cfg
}

// reportFaults writes the whole list of §11.3 where whoever started the service can
// read it.
//
// ALL of them and not the first: a volunteer who came to fix one file should leave
// having fixed it, and not discover the second fault after a restart.
func reportFaults(out io.Writer, path string, faults []domain.Fault) {
	fmt.Fprintf(out, "openscale : %s comporte %d faute(s) — le poste démarre en configuration "+
		"d'usine (ERR-CFG-01) et sert l'écran d'administration :\n", path, len(faults))
	for _, fault := range faults {
		fmt.Fprintf(out, "  %s\n", fault.String())
	}
}

// templatesFor resolves the label layouts this station runs on, with the operator's
// offset RECOMPOSED into the geometry.
//
// THE OFFSET IS CARRIED BY THE TEMPLATE AND BY NOTHING ELSE. printer.options.offset_x
// looks like the <A3> command of the printer language and it is not: the template is
// the only one of the two that the preview screen, the PDF export and the raster driver
// all show, so a volunteer pressing the ±1 dot arrow sees the label move. Feeding both
// would move it twice, and internal/printing/raster refuses such a job outright — see
// the godoc of checkTheOffsetIsAppliedOnce.
func templatesFor(cfg domain.Config, reg domain.Registries) (map[string]domain.Template, error) {
	offsetX, _ := cfg.Printer.Options.Int(optionOffsetX)
	offsetY, _ := cfg.Printer.Options.Int(optionOffsetY)

	shipped := domain.ShippedTemplates()
	out := make(map[string]domain.Template, len(shipped))
	for name, template := range shipped {
		template.OffsetXDots = int(offsetX)
		template.OffsetYDots = int(offsetY)
		out[name] = template
	}
	if _, ok := out[cfg.Printer.Template]; !ok {
		return nil, &serviceFailure{Exit: exitFailure, Message: fmt.Sprintf(
			"printer.template : gabarit inconnu %q ; gabarits disponibles : %s",
			cfg.Printer.Template, strings.Join(reg.TemplateNames(), ", "))}
	}
	return out, nil
}

// buildScale opens the weight source this configuration names, and NEVER refuses to
// start over it.
//
// A scale that cannot be opened is an amber light and a fallback to manual entry, never
// a station that will not start (guiding principle 7): Station.Start already degrades
// on a Start that fails, and this covers the step before — a protocol no driver of this
// binary answers to, or options it cannot read.
func buildScale(cfg domain.Config, reg *scale.Registry, clk ports.Clock, log ports.TechnicalLog) ports.Scale {
	weigher, err := newScale(cfg, reg, clk, log)
	if err != nil {
		log.Technical(domain.LevelError, "scale", "ERR-SCL-03",
			"La balance déclarée n'a pas pu être construite : le poste passe en saisie manuelle.",
			err.Error())
		return absent.New(log)
	}
	return weigher
}

// newScale builds the weight source of one configuration.
//
// A station that declares it has NO scale gets the absent source, and that is an
// explicit declaration rather than an inference: scale.present false turns the light
// off instead of leaving it red, and typing the weight becomes nominal (§9.3).
func newScale(cfg domain.Config, reg *scale.Registry, clk ports.Clock, log ports.TechnicalLog) (ports.Scale, error) {
	if !cfg.Scale.Present {
		return absent.New(log), nil
	}
	return reg.New(cfg.Scale.Type, cfg.Scale.Options, clk, log)
}

// buildPrinter builds the printer this configuration names, and never refuses to start
// over it either.
//
// The station keeps serving with a printer that says, in French, why it cannot print:
// the weighing is still journalled, the reprint bar is still there, and the
// administration screen names the offending key. A station that refused to start over a
// print queue would be a station nobody can reconfigure.
func buildPrinter(cfg domain.Config, reg *printing.Registry, templates map[string]domain.Template,
	clk ports.Clock, log ports.TechnicalLog, dataDir string) ports.Printer {
	printer, err := newPrinter(cfg, reg, templates, clk, log, dataDir)
	if err != nil {
		log.Technical(domain.LevelError, "printer", "ERR-PRN-01",
			"L'imprimante déclarée n'a pas pu être construite.", err.Error())
		return unbuiltPrinter{reason: err.Error()}
	}
	return printer
}

// newPrinter builds the print service of one configuration: the driver
// printer.type names, over the transport printer.options.transport names, with the
// retries and the roll counter of §8.2 around it.
func newPrinter(cfg domain.Config, reg *printing.Registry, templates map[string]domain.Template,
	clk ports.Clock, log ports.TechnicalLog, dataDir string) (ports.Printer, error) {
	byteLayer, err := newTransport(cfg.Printer.Options, clk, labelsDir(dataDir))
	if err != nil {
		return nil, err
	}
	driver, err := reg.New(cfg.Printer.Type, printing.DriverConfig{
		Options:   cfg.Printer.Options,
		Transport: byteLayer,
		Template:  templates[cfg.Printer.Template],
		Clock:     clk,
		Log:       log,
		DemoLabel: func() (domain.Label, error) { return demoLabel(cfg.Pricing) },
	})
	if err != nil {
		_ = byteLayer.Close()
		return nil, err
	}
	capacity, _ := cfg.Printer.Options.Int(optionRollCapacity)
	service, err := printing.NewService(printing.ServiceOptions{
		Main:     driver,
		MainName: byteLayer.Describe(),
		Clock:    clk,
		// The roll counter has NO persistent store yet: internal/store carries no
		// AddLabels/SetLabels pair, so the count restarts with the process. What it
		// still buys is the 90 % amber light within one service, and « J'ai changé le
		// rouleau » still resets it.
		Roll: printing.NewRollCounter(nil, int(capacity), log),
		Log:  log,
	})
	if err != nil {
		// Returned as a NIL INTERFACE and never as a typed nil pointer: a caller
		// checking `printer != nil` on a *Service that failed to build would get true.
		_ = driver.Close()
		return nil, err
	}
	return service, nil
}

// unbuiltPrinter is what a station has when its configuration names a printer this
// binary cannot build.
//
// It exists so that the station STARTS anyway: station.New requires a printer, and a
// nil one would take the whole poste out of service over a queue name. Every refusal it
// answers is KindConfig — no retry, and the administration screen shows what is
// configured against what actually exists (§8.5).
type unbuiltPrinter struct{ reason string }

// Descriptor reports a driver that exists in name only.
func (p unbuiltPrinter) Descriptor() domain.PrinterDescriptor {
	return domain.PrinterDescriptor{ID: "unbuilt", Label: "Imprimante non construite"}
}

// Print refuses, naming why the printer could not be built.
func (p unbuiltPrinter) Print(context.Context, ports.PrintJob) (ports.PrintReceipt, error) {
	return ports.PrintReceipt{}, p.refuse("printer.Print")
}

// Status reports that nothing can be known about a printer that was never opened.
func (p unbuiltPrinter) Status(context.Context) ports.PrinterStatus {
	return ports.PrinterStatus{Health: ports.PrinterFaulted,
		Detail: "l'imprimante configurée n'a pas pu être construite : " + p.reason}
}

// SelfTest refuses, for the same reason Print does.
func (p unbuiltPrinter) SelfTest(context.Context, string) error { return p.refuse("printer.SelfTest") }

// Close has nothing to release.
func (p unbuiltPrinter) Close() error { return nil }

func (p unbuiltPrinter) refuse(op string) error {
	return &ports.PrintError{Kind: ports.KindConfig, Op: op, Message: "l'imprimante configurée " +
		"n'a pas pu être construite : " + p.reason}
}

// technicalSink adapts the store to what the station writes technical lines through.
//
// Two structures that carry the same six values, and the conversion is the price of cut
// 3 of §5.2: internal/station names no storage type, so it declares what it needs and
// the composition root joins the two.
type technicalSink struct{ db *store.DB }

// RecordTechnical appends one line to the persisted technical journal.
func (s technicalSink) RecordTechnical(ctx context.Context, e station.TechnicalEntry) error {
	return s.db.RecordTechnical(ctx, store.TechnicalEntry{
		OccurredAt: e.At, Level: e.Level, Source: e.Source,
		Code: e.Code, Message: e.Message, Detail: e.Detail,
	})
}

// relayLog is the technical journal the drivers are given BEFORE the Hub that owns one
// exists.
//
// The interval is real and short — a driver is built, then the station, then the Hub —
// and a driver that reported a bad option during it would otherwise report it into
// nothing. Until the Hub is attached the lines go to the console, where whoever started
// the service by hand can read them; afterwards they go where every other line goes.
type relayLog struct {
	fallback io.Writer

	mu     sync.RWMutex
	target ports.TechnicalLog
}

// attach points the relay at the journal of the running station.
func (l *relayLog) attach(target ports.TechnicalLog) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.target = target
}

// Technical records one event.
func (l *relayLog) Technical(level, source, code, message, detail string) {
	l.mu.RLock()
	target := l.target
	l.mu.RUnlock()
	if target != nil {
		target.Technical(level, source, code, message, detail)
		return
	}
	fmt.Fprintf(l.fallback, "openscale [%s] %s %s : %s %s\n", level, source, code, message, detail)
}

// heldServer is the HTTP server as Station.Stop sees it, handed over after the station
// was built.
//
// station.Options.Server is fixed at construction and the server cannot exist before
// the Hub whose subscribers it closes on shutdown. Rather than move the shutdown
// sequence out of Station.Stop — which is where §13.4 is written and tested — the
// composition root hands over a holder and fills it one line later.
type heldServer struct {
	mu     sync.RWMutex
	server *http.Server
}

// hold puts the server in place. It is called once, before anything can serve.
func (h *heldServer) hold(server *http.Server) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.server = server
}

// Shutdown stops accepting and waits for the active requests, up to ctx.
//
// A holder that was never filled shuts nothing down and says so with a nil error: the
// station failed before it ever served, and a shutdown that reported a failure there
// would name a server that does not exist.
func (h *heldServer) Shutdown(ctx context.Context) error {
	h.mu.RLock()
	server := h.server
	h.mu.RUnlock()
	if server == nil {
		return nil
	}
	return server.Shutdown(ctx)
}

// imagesRoot is the photo directory of §11.1, laid out as
// <2 first characters of the sha>/<sha>.<detected extension> (§10.7).
func imagesRoot(dataDir string) string { return filepath.Join(dataDir, "images") }

// imagesDir is that directory as the HTTP layer reads it.
func imagesDir(dataDir string) fs.FS { return os.DirFS(imagesRoot(dataDir)) }

// labelsDir is where the `file` transport drops one copy per label (§11.1).
func labelsDir(dataDir string) string { return filepath.Join(dataDir, "labels") }
