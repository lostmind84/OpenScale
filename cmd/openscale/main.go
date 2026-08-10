// Command openscale is the single binary of the weighing station.
//
// Every subcommand lives in this package and shares one process: there is no
// runtime to install and no second executable to deploy. A station is set up by
// copying one file.
//
// The subcommands of the V1 scope are serve, kiosk, doctor, capture, replay,
// label, config and service. barcode and price are the DEMONSTRATION
// commands of the first work package: they exercise the business core from a
// terminal, with no scale, no printer and no browser. capture and replay are the
// diagnostic pair of the third: capture needs a scale on the bench, replay needs
// nothing but a file. label is the demonstration of the fourth, and it needs no
// printer either: the PDF it writes is measured with a ruler.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

// Injected by the linker (see the Makefile). The zero values are what a plain
// `go build` produces, and they say so rather than pretending to be a release.
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

// usage stays FRENCH: it is read by whoever runs the binary, and the audience of
// this project is a cooperative, not a Go developer.
const usage = `openscale — poste de pesée libre-service

Usage :
  openscale <commande> [options]

Le service :
  serve [--config f] [--data d]            lance le poste : balance, imprimante, base
        [--listen hôte:port]               et écran client. C'est ce que démarre le
                                           service Windows ou l'unité systemd
  kiosk [--config f] [--url adresse]       ouvre l'écran client en plein écran et le
        [--profile répertoire]             relance s'il se ferme. C'est la tâche
                                           planifiée « OpenScale-Kiosk »
  service install|uninstall|start|stop|status
        [--start auto|demand]              enregistre le poste comme service Windows.
        [--config f] [--data d]            Sous Linux, c'est l'unité systemd

La configuration (§11.5) :
  config validate [fichier]                liste TOUTES les fautes, en français
  config export [fichier] [--hardware]     la configuration à cloner vers les autres
                [--output f.json]          postes — sans le bloc matériel par défaut
  config fingerprint [fichier]             l'empreinte de 8 caractères à comparer
  config station [fichier] --number <n>    pose l'identité du poste et l'état de sa
                 --name <texte>            balance, sans passer par l'écran : c'est ce
                 [--no-scale]              que fait l'installeur sur un poste neuf
  config password [fichier]                pose le mot de passe d'administration, lu sur
                                           l'entrée standard et jamais en argument
  config recovery-code [fichier]           tire le code de secours de la fiche
  config migrate [fichier]                 remet le fichier à la forme que ce binaire lit

Diagnostic du poste (lot L8) :
  doctor [--zip] [--output f.zip]          les contrôles de §15.4 : ce qui a été
         [--config f] [--data d]           vérifié, le verdict, et ce qu'il faut FAIRE si
         [--listen hôte:port]              c'est rouge. Fonctionne même quand le service
                                           ne démarre pas. --zip écrit en plus le fichier
                                           de diagnostic à envoyer au support

Commandes de démonstration du lot L1 :
  barcode <référence> --weight <grammes>   génère le code-barres EAN-13 d'une pesée
  barcode <référence> --units <nombre>     idem pour un produit vendu à l'unité
  price --unit-price <prix> --weight <g>   calcule les prix d'une pesée

Diagnostic de la balance (lot L3) :
  capture --port COM8 --duration 30m       dump hexa + ASCII du port série, et
                                           mesure la cadence réelle d'émission
  replay <fichier> [--x10]                 rejoue un fichier de trames : poids,
                                           état de figeage, cadence médiane

Aperçu de l'étiquette, sans imprimante (lot L4) :
  label --template <nom> --demo [--dual]   rend l'étiquette de démonstration en PDF
                                           grandeur nature et en PNG. Imprimé à
                                           100 %, le PDF se mesure au réglet
        [--pdf f.pdf] [--png f.png]        emplacements des fichiers ; sans eux,
                                           les deux portent le nom du gabarit
        [--annotate]                       surcouche de banc : zone imprimable,
                                           zones de silence et réglet millimétrique

Autres :
  --version                                version, commit et date de compilation
  --help                                   ce message

Exemples :
  openscale doctor
  openscale doctor --zip
  openscale config export --hardware=false --output config-poste2.json
  openscale config fingerprint
  openscale barcode 0493021000003 --weight 1236
  openscale price --unit-price 5,32 --weight 1236 --tiers cagette
  openscale capture --port COM8 --duration 30m
  openscale replay frames.txt --read-size 18
  openscale label --template weighing_identical --demo --dual --pdf etiquette.pdf
`

// parseMixed lets options appear BEFORE or AFTER the positional arguments.
//
// The standard flag package stops at the first non-flag, so
// `barcode 0493021000003 --weight 1236` would leave --weight unparsed. Nobody
// types the options first, and the demonstration commands of the documentation do
// not either.
func parseMixed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return positional, nil
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "serve":
		// Supervised, not bare: the SAME station, plus whatever the supervisor that
		// started it needs to hear — the Windows service control protocol, or the READY=1
		// and the watchdog of a systemd unit (§15.2, §15.3). Started from a terminal it is
		// exactly `serve` and nothing else happens.
		err = runServeSupervised(stopSignals(), os.Args[2:], os.Stdout)
	case "kiosk":
		err = runKiosk(stopSignals(), os.Args[2:], os.Stdout)
	case "service":
		err = runService(os.Args[2:], os.Stdout)
	case "config":
		err = runConfig(os.Args[2:], os.Stdin, os.Stdout)
	case "doctor":
		err = runDoctor(stopSignals(), os.Args[2:], os.Stdout)
	case "barcode":
		err = runBarcode(os.Args[2:], os.Stdout)
	case "price":
		err = runPrice(os.Args[2:], os.Stdout)
	case "capture":
		err = runCapture(os.Args[2:], os.Stdout)
	case "replay":
		err = runReplay(os.Args[2:], os.Stdout)
	case "label":
		err = runLabel(os.Args[2:], os.Stdout)
	case "--version", "version":
		fmt.Printf("openscale %s (commit %s, compilé le %s)\n", version, commit, date)
	case "--help", "-h", "help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "openscale : commande inconnue %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}

	if err != nil {
		// One sentence on stderr. A volunteer reading this has no stack trace to make
		// sense of, and the message names what to fix. The CODE is not always 1: §13.4
		// reserves 3 for a station that cannot take its socket or stopped serving, and
		// that is what the service manager reads to tell « it failed » from « another
		// one is already running ».
		fmt.Fprintf(os.Stderr, "openscale : %s\n", explain(err))
		os.Exit(exitCodeFor(err))
	}
}

// stopSignals is the context that ends when the service is asked to stop.
//
// SIGTERM is what systemd sends and what `sc stop` becomes on Windows; Ctrl+C is what
// whoever is standing in front of the station presses. Both lead to the SAME ordered
// shutdown of §13.4, because a station that stopped differently depending on who asked
// would have two shutdowns and one of them would be untested.
func stopSignals() context.Context {
	ctx, _ := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	return ctx
}
