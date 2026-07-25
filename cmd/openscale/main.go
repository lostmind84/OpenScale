// Command openscale is the single binary of the weighing station.
//
// Every subcommand lives in this package and shares one process: there is no
// runtime to install and no second executable to deploy. A station is set up by
// copying one file.
//
// The subcommands of the V1 scope are serve, kiosk, doctor, capture, replay,
// label and config. Five are present. barcode and price are the DEMONSTRATION
// commands of the first work package: they exercise the business core from a
// terminal, with no scale, no printer and no browser. capture and replay are the
// diagnostic pair of the third: capture needs a scale on the bench, replay needs
// nothing but a file. label is the demonstration of the fourth, and it needs no
// printer either: the PDF it writes is measured with a ruler.
package main

import (
	"flag"
	"fmt"
	"os"
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
		// One sentence on stderr and exit code 1. A volunteer reading this has no
		// stack trace to make sense of, and the message names what to fix.
		fmt.Fprintf(os.Stderr, "openscale : %s\n", explain(err))
		os.Exit(1)
	}
}
