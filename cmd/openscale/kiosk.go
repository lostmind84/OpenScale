package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"runtime"
	"strings"

	"openscale/internal/domain"
	"openscale/internal/kiosk"
	"openscale/internal/platform"
)

// runKiosk is the subcommand of §15.1 that shows the client screen: the second
// process of §3, the one the scheduled task of §15.2 starts at logon and the one
// `cage` starts on tty1 under Linux (§15.3).
//
// It does NOTHING but HTTP. It holds no serial port, opens no database, and prints no
// label: everything it knows about the station it learns from one URL. That is what
// makes it restartable at will, and what makes « the browser died » a non-event.
func runKiosk(ctx context.Context, args []string, out io.Writer) error {
	// What the option parsing has to say is kept aside until the journal exists. It
	// cannot be opened any earlier — its own path is one of the options — and « la
	// configuration est illisible » is exactly the line somebody looks for afterwards.
	var parsed bytes.Buffer
	options, err := parseKioskOptions(args, io.MultiWriter(out, &parsed))
	if err != nil {
		return err
	}

	if options.logPath != "" {
		journal, err := kiosk.OpenLog(options.logPath, kiosk.DefaultLogSize)
		if err != nil {
			// A journal is a diagnostic aid, never a reason to leave a customer in front
			// of a black screen. The sentence goes to the stream that is left.
			fmt.Fprintf(out, "openscale kiosk : %v\n", err)
		} else {
			defer func() { _ = journal.Close() }()
			_, _ = journal.Write(parsed.Bytes())
			out = io.MultiWriter(out, journal)
		}
	}

	browser, found := kiosk.Find(browserCandidates(), kiosk.LookBrowser(programDirectories()))
	if !found {
		// The one failure no relaunch fixes, and the only one this subcommand refuses
		// to start on. The sentence names the three executables and where to get one,
		// because whoever reads it is standing in front of a black screen.
		return fmt.Errorf("aucun navigateur trouvé sur ce poste (cherchés : %s). "+
			"Installez Microsoft Edge, Google Chrome ou Chromium, puis relancez la tâche "+
			"« %s »", strings.Join(browserCandidates(), ", "), taskName)
	}

	// BEFORE the supervisor, because the supervisor launches the browser and a browser
	// reads its policies when it starts. Posed at every logon rather than once at
	// installation: this process runs as the station account, so it writes in its own
	// hive — no other account of the PC is bound — and a key somebody deleted is back at
	// the next start.
	posePolicies(browser, options.url, out)

	supervisor, err := kiosk.New(kiosk.Options{
		URL:        options.url,
		Browser:    browser,
		ProfileDir: options.profileDir,
		Launch:     kiosk.ExecLauncher,
		Alive:      kiosk.AliveProbe(options.url),
		Attached:   kiosk.AttachedProbe(options.url),
		Awake:      platform.KeepAwake,
		Clock:      platform.NewSystemClock(),
		Out:        out,
	})
	if err != nil {
		return err
	}

	// Here and not at the top of the function, because everything above can still fail
	// with a sentence somebody has to read -- « aucun navigateur trouvé sur ce poste »
	// is reported by nothing else. Masking the window earlier would send those to a
	// window that has left the screen. From this line on, the supervisor writes to its
	// journal and the console has nothing left to show.
	if err := platform.HideOwnConsole(); err != nil {
		// A console window left on screen is ugly; it is never a reason to leave a
		// customer in front of a black screen. The station starts, the journal says why.
		fmt.Fprintf(out, "openscale kiosk : la fenêtre de console n'a pas pu être masquée (%v)\n", err)
	}
	return supervisor.Run(ctx)
}

// posePolicies writes the navigation policies of §15.2 for the account that runs the
// kiosk, and says in one line what happened.
//
// It NEVER stops the station. Everything it can fail on is a station that is a little less
// locked down — a browser this table does not know, a hive that refuses a write — and a
// black screen would be a far worse answer to any of them. The line it writes is what
// somebody reads the day a poste is found on a search engine, and the presence watch of
// the supervisor is what brings that poste back meanwhile.
func posePolicies(browser kiosk.Browser, url string, out io.Writer) {
	root := kiosk.PolicyRoot(browser)
	if root == "" {
		fmt.Fprintf(out, "openscale kiosk : %s n'a pas de stratégie de navigation connue — "+
			"l'écran client n'est pas verrouillé sur l'application\n", browser.Name)
		return
	}
	written, err := platform.WriteUserPolicies(root, kiosk.Policies(url))
	if err != nil {
		fmt.Fprintf(out, "openscale kiosk : les stratégies de navigation n'ont pas pu être "+
			"posées (%v) — l'écran client n'est pas verrouillé sur l'application\n", err)
		return
	}
	if written == 0 {
		return
	}
	fmt.Fprintf(out, "openscale kiosk : %d stratégies de navigation posées sous %s\n", written, root)
}

// kioskOptions is what `openscale kiosk` was told, once the flags, the configuration
// file and the defaults have been resolved.
type kioskOptions struct {
	url        string
	profileDir string
	// logPath is where the supervisor's lines are kept. Empty means « nowhere but the
	// standard output », which is what a station under systemd already has.
	logPath string
}

// parseKioskOptions resolves the address of the client screen.
//
// The address comes from the SAME configuration file the service reads, and that is
// the point: `network.listen` is written in one place, and a kiosk carrying its own
// copy would be a station that shows a blank page the day somebody moves the port on
// the screen that exists for moving it (§11.4).
func parseKioskOptions(args []string, out io.Writer) (kioskOptions, error) {
	fs := flag.NewFlagSet("kiosk", flag.ContinueOnError)
	fs.SetOutput(out)
	var (
		configPath = fs.String("config", os.Getenv("OPENSCALE_CONFIG"), "fichier de configuration")
		address    = fs.String("url", "", "adresse de l'écran client, sinon celle de la configuration")
		profile    = fs.String("profile", "", "répertoire de profil du navigateur")
		logPath    = fs.String("log", platform.DefaultKioskLogPath(), "journal du superviseur")
	)
	fs.Usage = func() {
		fmt.Fprint(out, `Usage : openscale kiosk [--config fichier] [--url http://hôte:port] [--profile répertoire] [--log fichier]

Le superviseur de navigateur : il ouvre l'écran client en plein écran et le relance
s'il se ferme. C'est ce que lance la tâche planifiée « `+taskName+` » à l'ouverture de
session sous Windows, et l'unité openscale-kiosk.service sous Linux.

Options :
  --config <fichier>       configuration du poste, dont l'adresse d'écoute est lue ;
                           sinon OPENSCALE_CONFIG, sinon l'emplacement par défaut
  --url <adresse>          adresse de l'écran client ; prioritaire sur la configuration
  --profile <répertoire>   profil dédié du navigateur, effacé à chaque démarrage ;
                           sinon un répertoire sous le dossier temporaire du compte
  --log <fichier>          journal du superviseur, en plus de la sortie standard ;
                           --log "" ne journalise que sur la sortie standard
`)
	}
	positional, err := parseMixed(fs, args)
	if err != nil {
		return kioskOptions{}, err
	}
	if len(positional) != 0 {
		fs.Usage()
		return kioskOptions{}, fmt.Errorf("argument inattendu %q : kiosk ne prend que des options", positional[0])
	}

	o := kioskOptions{url: *address, profileDir: *profile, logPath: *logPath}
	if o.profileDir == "" {
		o.profileDir = kiosk.DefaultProfileDir()
	}
	if o.url != "" {
		return o, nil
	}

	path := *configPath
	if path == "" {
		path = platform.DefaultConfigPath()
	}
	cfg, _, faults, err := platform.LoadConfig(path)
	switch {
	case err != nil:
		// A kiosk that refused to start because the configuration is unreadable would
		// black out the screen of a station whose administration page is exactly what
		// somebody needs to reach in order to fix it (§11.3). The neutral profile
		// carries the default address, and the station serves its fault list on it.
		fmt.Fprintf(out, "openscale kiosk : configuration %s illisible (%v) — "+
			"ouverture sur l'adresse par défaut\n", path, err)
		cfg = domain.NeutralProfile()
	case len(faults) > 0:
		// The file itself was read; at least one block of it was not -- exactly what the
		// old, strict decode used to refuse whole. cfg already carries whatever
		// DecodeConfigBlockByBlock could recover, address included, so this only says why
		// the kiosk may be opening somewhere a volunteer did not expect.
		fmt.Fprintf(out, "openscale kiosk : configuration %s illisible ou invalide (%d "+
			"faute(s) de décodage) — ouverture sur la meilleure adresse qui a pu être lue\n",
			path, len(faults))
	}
	o.url = clientScreenURL(cfg.Network.Listen)
	return o, nil
}

// clientScreenURL turns a listen address into the URL a browser opens.
//
// An address bound to every interface — 0.0.0.0:8085, :8085 — is not an address a
// browser can dial: the kiosk is on the machine itself, so the loopback is both
// correct and the only choice that works when admin_on_lan is on (§11.2).
func clientScreenURL(listen string) string {
	host, port := splitListen(listen)
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "127.0.0.1"
	}
	return (&url.URL{Scheme: "http", Host: host + ":" + port}).String()
}

// splitListen separates the host from the port of a listen address, tolerating the
// forms a human types.
func splitListen(listen string) (host, port string) {
	listen = strings.TrimSpace(listen)
	if listen == "" {
		return "127.0.0.1", "8085"
	}
	index := strings.LastIndex(listen, ":")
	if index < 0 {
		return listen, "8085"
	}
	host, port = listen[:index], listen[index+1:]
	if port == "" {
		port = "8085"
	}
	return host, port
}

// browserCandidates is the search order of §15.2 for this platform.
func browserCandidates() []string {
	if runtime.GOOS == "windows" {
		return kiosk.WindowsCandidates
	}
	return kiosk.LinuxCandidates
}

// programDirectories are the roots a browser is looked for under when it is not on the
// PATH.
//
// They are read from the environment rather than spelled here: a station whose system
// drive is not C: has other roots, and §11.1 leaves exactly one place in the repository
// allowed to spell a default path.
func programDirectories() []string {
	if runtime.GOOS != "windows" {
		return nil
	}
	var roots []string
	for _, variable := range []string{"ProgramFiles", "ProgramFiles(x86)", "LOCALAPPDATA"} {
		if root := os.Getenv(variable); root != "" {
			roots = append(roots, root)
		}
	}
	return roots
}
