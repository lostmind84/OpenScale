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

	catalogpkg "openscale/internal/catalog"
	"openscale/internal/catalog/importer"
	"openscale/internal/diag"
	"openscale/internal/domain"
	"openscale/internal/platform"
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
//
// What this file no longer holds, and where it went: the exit codes and the ERR-SYS
// sentences are in failure.go, the socket in listen.go, the neutral profile of §11.3
// in fallback.go, the templates and the two devices in wiring.go, the three holders in
// adapters.go, and the layout of the data directory in paths.go.

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

	// restarting hands over the demand the restart button carries, and is nil in
	// production.
	//
	// It exists because the ROUTE cannot prove what this file does: restarterFor gives
	// the HTTP layer nothing on a station nobody supervises, and a test binary is never
	// supervised — so a request would honestly answer 501 and prove nothing about the
	// exit code, which is the whole point of the mechanism. What the route does with the
	// demand is proven in internal/web; what serve() does with it is proven here.
	restarting func(ask func() error)
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
	// A malformed --listen is refused HERE, naming the flag, because from any later point
	// the only thing left to blame is the file: the flag used to be written into the
	// configuration before it was validated, so `--listen 8085` took a healthy station out
	// of service and reported « configuration d'usine (ERR-CFG-01) » about config.json.
	// The rule is domain's own, control 2's, and not a second one that would drift from it.
	if o.listen != "" {
		if err := domain.CheckListenAddress(o.listen); err != nil {
			return serveOptions{}, &serviceFailure{Exit: exitFailure, Message: fmt.Sprintf(
				"--listen %q n'est pas une adresse hôte:port valide (%s) ; attendu hôte:port, "+
					"par exemple 127.0.0.1:8085", o.listen, err)}
		}
	}
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

	cfg, notes, decodeFaults, err := platform.LoadConfig(o.configPath)
	if err != nil {
		return &serviceFailure{Exit: exitFailure, Err: err, Message: fmt.Sprintf(
			"le fichier de configuration %s ne peut pas être lu : %v", o.configPath, err)}
	}
	reportMigration(out, o.configPath, notes)
	// The service OWNS its data directory and creates it (§15.3). Nothing here is a
	// mount point and nothing here is shared: an administrator who wants files to
	// arrive from a share mounts what they like where they like and synchronises into
	// it, and the unit has nothing to know about it.
	for _, dir := range []string{
		o.dataDir, imagesRoot(o.dataDir), labelsDir(o.dataDir), previewsDir(o.dataDir),
	} {
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
	faults := append(decodeFaults, cfg.Validate(registries)...)
	outOfService := len(faults) > 0
	if outOfService {
		reportFaults(out, o.configPath, faults)
		cfg = fallbackProfile(cfg, faults)
	}
	// --listen is applied AFTER the verdict, and that is the whole point of its position.
	// Written before, it was judged as if the file had carried it: a typo on the command
	// line took a healthy station out of service and was reported against config.json,
	// while a genuine fault in the file's own address was quietly repaired for the
	// duration of the run and came back at the next restart.
	if o.listen != "" {
		cfg.Network.Listen = o.listen
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
	catalogAt := lastCatalogImport(ctx, db, log)

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
		guardFunc(func() (bool, string) { return st.Hub().DowntimeGuard() }), o.dataDir)

	st, err = station.New(station.Options{
		Clock:        clock,
		Config:       cfg,
		Catalog:      catalog,
		CatalogAt:    catalogAt,
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
		//
		// What arrives here is THE FILE AS IT WAS BEFORE THE SAVE, and never the
		// configuration the station was running. The two differ on exactly the station this
		// matters for: one whose file is unusable runs the neutral profile (§11.3) while
		// its file keeps the cooperative's own settings, and writing the running
		// configuration back replaced them with the factory ones — the tariffs, the
		// safeguards, the categories — sixty seconds after a volunteer repaired the file.
		OnRevert: func(fileBefore domain.Config) {
			if err := configFile.Save(context.Background(), fileBefore); err != nil {
				log.Technical(domain.LevelError, "config", "",
					"Retour arrière appliqué au poste mais non écrit : le fichier porte "+
						"encore la configuration non confirmée.", err.Error())
				return
			}
			// The detail names BOTH fingerprints. « Retour à la version précédente »
			// designates two documents on an out-of-service station, and a line carrying
			// one number would leave a volunteer unable to say which one came back.
			running := st.Hub().Config()
			log.Technical(domain.LevelWarn, "config", "",
				"Configuration non confirmée : le fichier est revenu à la version précédente.",
				fmt.Sprintf("fichier %s, poste %s", fileBefore.Fingerprint(), running.Fingerprint()))
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

	// The restart button, wired to the select below. It is built here and not inside
	// web.Options because serve() is the half that WAITS on it.
	restarter := newStationRestarter(func() (bool, string) { return st.Hub().DowntimeGuard() })

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
			clock: clock, hub: st.Hub(), registries: registries, scales: scales,
			technical: st.Hub().TechnicalLog(), config: st.Hub().Config,
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
		Update: updaterFor(updateService),
		// Nil on a station nobody supervises, and the route then says so rather than
		// stopping a process that would stay stopped (restarterFor).
		Restart: restarterFor(restarter),
		// The thirty-second countdown before the machine goes down lives in the HTTP
		// layer, on the injected clock; this is only what it calls at the end.
		Reboot: rebooterFor(),
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
	if o.restarting != nil {
		o.restarting(restarter.Restart)
	}
	if o.serving != nil {
		o.serving(binder.Addr().String())
	}

	var fatal error
	select {
	case <-ctx.Done():
	case <-restarter.Asked():
		// A stop somebody asked for, which takes the SAME road as every other stop —
		// the ordered shutdown below is not duplicated for this case, and that is the
		// point of going through here rather than through a script.
		fatal = &serviceFailure{Code: codeRestartAsked, Exit: exitRestart, Message: "" +
			"Redémarrage demandé depuis l'écran d'administration. Le gestionnaire de " +
			"services relance le poste."}
		recordFailure(db, clock, fatal)
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

// lastCatalogImport reads back WHEN the catalog in the base was imported.
//
// The client screen shows that instant permanently, and it answers one question:
// « quand le catalogue a-t-il été importé et mis à jour pour la dernière fois ? »
// (§14.3). The station used to stamp it with the clock at start-up, so every reboot,
// every update and every crash recovery re-dated a catalog nobody had touched — and the
// date §14.3 relies on to reveal a station that receives nothing quietly caught up
// every morning instead.
//
// « Applied » and not « last »: a file identical to the one in service is recorded
// 'unchanged' and changes nothing, so it must not move the date either. The row this
// reads is written in the SAME TRANSACTION as the catalog, which is what makes the two
// impossible to disagree.
//
// A station that has never applied one gets the zero instant, and §14.3 has a sentence
// for that: « Catalogue en attente ».
func lastCatalogImport(ctx context.Context, db *store.DB, log ports.TechnicalLog) time.Time {
	last, err := db.LastAppliedImport(ctx)
	if err == nil {
		return last.OccurredAt
	}
	if !errors.Is(err, store.ErrNotFound) {
		// Not fatal, and deliberately: a station whose history cannot be read still
		// weighs, prints and serves its grid. What it loses is one line of a status bar.
		log.Technical(domain.LevelWarn, "catalog", "",
			"Date du dernier import illisible : l'écran client ne datera pas sa grille.",
			err.Error())
	}
	return time.Time{}
}
