package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"openscale/internal/kiosk"
	"openscale/internal/platform"
	"openscale/internal/station"
	"openscale/internal/station/ports"
)

// taskName is the scheduled task of §15.2 that starts the kiosk at logon.
//
// It is named here rather than only in the installer because two French sentences the
// binary prints name it, and a task name that drifted between a script and a message
// would send a volunteer looking in the wrong list.
const taskName = "OpenScale-Kiosk"

// serviceDescription is what the services console of Windows shows, in French.
const serviceDescription = "Poste de pesée libre-service : balance, imprimante, catalogue et écran client. " +
	"L'écran lui-même est affiché par la tâche « " + taskName + " » à l'ouverture de session."

// The restarts of §15.2, which `sc failure` used to write.
//
// Five seconds, ten, thirty: increasing, because a station that fails immediately
// three times in a row is failing for a reason a fourth attempt one second later will
// not fix — and the counter is reset after a full day up, so a crash next month starts
// over at five seconds.
var (
	serviceRecoveryDelays = []time.Duration{5 * time.Second, 10 * time.Second, 30 * time.Second}
	serviceRecoveryReset  = 24 * time.Hour
)

// runService is `openscale service install|uninstall|start|stop|status` (§15.1).
//
// It exists so that `install.ps1` has ONE guarded native call to make instead of three
// — `sc.exe create`, `sc.exe config`, `sc.exe failure` — and §15.2 explains why that
// matters more than it looks: `$ErrorActionPreference = 'Stop'` does not catch a native
// executable, so each of those three can fail silently and let the script announce a
// successful installation.
func runService(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("service", flag.ContinueOnError)
	fs.SetOutput(out)
	var (
		configPath = fs.String("config", "", "fichier de configuration passé au service")
		dataDir    = fs.String("data", "", "répertoire de données passé au service")
		start      = fs.String("start", "auto", "démarrage du service : auto ou demand")
	)
	fs.Usage = func() { fmt.Fprint(out, serviceUsage) }

	positional, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	if len(positional) != 1 {
		fs.Usage()
		return errors.New("service prend une action : install, uninstall, start, stop ou status")
	}

	clock := platform.NewSystemClock()
	switch positional[0] {
	case "install":
		return installService(out, *configPath, *dataDir, *start)
	case "uninstall":
		if err := platform.RemoveService(clock, platform.ServiceName, serviceStopBudget()); err != nil {
			return err
		}
		fmt.Fprintf(out, "service %s retiré. Les données et la configuration sont intactes.\n",
			platform.ServiceName)
		return nil
	case "start":
		if err := platform.StartService(platform.ServiceName); err != nil {
			return err
		}
		fmt.Fprintf(out, "service %s démarré.\n", platform.ServiceName)
		return nil
	case "stop":
		if err := platform.StopService(clock, platform.ServiceName, serviceStopBudget()); err != nil {
			return err
		}
		fmt.Fprintf(out, "service %s arrêté.\n", platform.ServiceName)
		return nil
	case "status":
		return reportServiceState(out)
	}
	fs.Usage()
	return fmt.Errorf("action inconnue %q : install, uninstall, start, stop ou status", positional[0])
}

const serviceUsage = `Usage : openscale service <install|uninstall|start|stop|status> [options]

Enregistre le poste comme service Windows, ou le retire. Sous Linux, ce travail est
celui de l'unité systemd livrée dans deploy/linux.

Actions :
  install     enregistre le service (idempotent : relancer ne casse rien)
  uninstall   arrête et retire le service, SANS toucher aux données
  start       démarre le service
  stop        arrête le service et attend qu'il soit vraiment arrêté
  status      dit si le service est installé, son mode de démarrage et son état

Options d'install :
  --start auto|demand      démarrage automatique, ou manuel pendant la période pilote
  --config <fichier>       configuration à passer au service ; sinon son défaut
  --data <répertoire>      répertoire de données à passer au service ; sinon son défaut
`

// installService registers the service with the arguments it will run with.
func installService(out io.Writer, configPath, dataDir, start string) error {
	if start != "auto" && start != "demand" {
		return fmt.Errorf("--start vaut auto ou demand, pas %q", start)
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("le chemin du binaire n'a pas pu être déterminé : %w", err)
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return fmt.Errorf("le chemin du binaire n'a pas pu être résolu : %w", err)
	}

	spec := platform.ServiceSpec{
		Name:        platform.ServiceName,
		DisplayName: "OpenScale — poste de pesée",
		Description: serviceDescription,
		Executable:  executable,
		Arguments:   serviceArguments(configPath, dataDir),
		AutoStart:   start == "auto",
		// The SCM is told the SAME budget systemd is (§13.4): it is the number
		// update.ps1 got wrong, and the reason it failed intermittently on a healthy
		// station.
		StopBudget:     serviceStopBudget(),
		RecoveryDelays: serviceRecoveryDelays,
		RecoveryReset:  serviceRecoveryReset,
	}
	if err := platform.InstallService(spec); err != nil {
		return err
	}
	mode := "automatique"
	if !spec.AutoStart {
		mode = "manuel"
	}
	fmt.Fprintf(out, "service %s installé, démarrage %s, budget d'arrêt %s.\n",
		spec.Name, mode, spec.StopBudget)
	fmt.Fprintf(out, "  commande : %s %v\n", spec.Executable, spec.Arguments)
	return nil
}

// serviceArguments is what the service will be started with.
//
// The two paths are only passed when they are NOT the defaults: a service whose
// command line repeats the default location is a service that keeps pointing at the old
// one the day the default moves.
func serviceArguments(configPath, dataDir string) []string {
	arguments := []string{"serve"}
	if configPath != "" && configPath != platform.DefaultConfigPath() {
		arguments = append(arguments, "--config", configPath)
	}
	if dataDir != "" && dataDir != platform.DefaultDataDir() {
		arguments = append(arguments, "--data", dataDir)
	}
	return arguments
}

// serviceStopBudget is how long a service manager must wait for the ordered shutdown
// of §13.4 before concluding the process hung.
//
// It is station.ShutdownBudget and never a number typed here: §13.4's own account of
// the bug is a budget written next to the code it was supposed to cover, and drifting
// from it. The 1.5 factor is for the two tails nobody bounds — an import transaction
// rolling back, and the WAL checkpoint of a journal that has grown.
func serviceStopBudget() time.Duration {
	return station.ShutdownBudget() * 3 / 2
}

// reportServiceState prints what the service manager knows, in French.
func reportServiceState(out io.Writer) error {
	state, err := platform.QueryService(platform.ServiceName)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "%s\n", state.Detail)
	if !state.Installed {
		return nil
	}
	if !state.Running {
		fmt.Fprintf(out, "  démarrez-le avec « openscale service start » ou depuis les services de Windows.\n")
	}
	return nil
}

// runServeSupervised is what `openscale serve` really is: the station, plus whatever
// the supervisor that started it needs to hear.
//
// There are three ways this binary is started and ONE station behind them. Typed into a
// terminal, it writes to stderr and stops on Ctrl+C. Started by the Windows SCM, it
// speaks the service control protocol — a binary that only did so behind an extra flag
// would fail to start for whoever forgets the flag, and no installation sheet fixes
// that. Started by systemd with Type=notify, it says READY=1 when it really serves and
// feeds the watchdog for as long as the Hub answers.
//
// Not one line of the composition root changes for any of it: `serve` already exposes
// the seam this needs — the callback it invokes once the socket is open — and it was
// there for the tests.
func runServeSupervised(ctx context.Context, args []string, out io.Writer) error {
	options, err := parseServeOptions(args, out)
	if err != nil {
		return err
	}

	clock := platform.NewSystemClock()
	notifier := platform.NewServiceNotifier(platform.SystemEnv, os.Getpid())
	defer func() { _ = notifier.Close() }()

	// The address is not known until the socket is open — network.listen may say :0 in a
	// test, and a station may have been given --listen. The watchdog therefore starts
	// from inside this callback, on the address the station REALLY serves.
	watchdog := make(chan string, 1)
	options.serving = func(address string) {
		select {
		case watchdog <- address:
		default:
		}
	}

	run := func(ctx context.Context) error {
		go func() {
			select {
			case <-ctx.Done():
			case address := <-watchdog:
				announceAndWatch(ctx, clock, notifier, address, out)
			}
		}()
		err := serve(ctx, options, out)
		// Said BEFORE the shutdown finishes draining the print worker, so that the time
		// §13.4 budgets is not read as a process that hung.
		_ = notifier.Stopping()
		return err
	}

	if platform.StartedByServiceManager() {
		return platform.RunAsService(platform.ServiceName, serviceStopBudget(), run)
	}
	return run(ctx)
}

// announceAndWatch tells the init system the station serves, then keeps telling it the
// Hub is alive.
//
// IT READS /healthz AND NEVER /readyz, and §15.3 makes that the most important rule of
// the section: an unplugged scale, a printer with no paper and a catalog that never
// arrived all answer /readyz with a 503, and a watchdog reading it would restart the
// station over a roll of labels — losing a customer's weighing to go and fetch one.
func announceAndWatch(ctx context.Context, clock ports.Clock, notifier *platform.ServiceNotifier,
	address string, out io.Writer) {
	alive := kiosk.AliveProbe("http://" + address)
	if !notifier.Enabled() {
		return
	}
	if err := notifier.Ready(); err != nil {
		fmt.Fprintf(out, "openscale : READY=1 n'a pas pu être envoyé : %v\n", err)
	}
	_ = notifier.Status(fmt.Sprintf("poste en écoute sur http://%s", address))

	interval := notifier.WatchdogInterval()
	if interval <= 0 {
		return
	}
	// A third of the period, which is what systemd's own documentation asks for: two
	// missed answers must not be enough to be killed, because one of them can be a
	// scheduler hiccup on a mini-PC.
	feed(ctx, clock, notifier, alive, interval/3, out)
}

// watchdogSink is the one method feeding needs, declared here so that the rule that
// matters — a station whose Hub stopped answering is NOT fed — is provable without a
// systemd socket, which no Windows development machine can even create.
type watchdogSink interface {
	// Alive feeds the watchdog for one period.
	Alive() error
}

// feed answers the watchdog for as long as the Hub does.
func feed(ctx context.Context, clock ports.Clock, notifier watchdogSink,
	alive func(context.Context) bool, period time.Duration, out io.Writer) {
	ticks, stop := clock.Ticker(period)
	defer stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			if !alive(ctx) {
				// Deliberately NOT fed. The unit restarts the station, which is the one
				// case a restart is the right answer: the loop that serves every
				// weighing stopped answering.
				fmt.Fprintf(out, "openscale : /healthz ne répond pas — le chien de garde n'est pas nourri\n")
				continue
			}
			if err := notifier.Alive(); err != nil {
				fmt.Fprintf(out, "openscale : WATCHDOG=1 n'a pas pu être envoyé : %v\n", err)
			}
		}
	}
}
