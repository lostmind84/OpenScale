package main

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"openscale/internal/domain"
)

// This file is `openscale config station`: WHO this station is — its number, its name —
// and the declaration that it has no scale yet.
//
// # Why a fourth door that writes the file
//
// The delivered configuration is the export of §11.5, so a station comes out of the
// archive with station.number 0, station.name empty and a scale block that names a
// protocol without naming a port. Every one of those is a fault, the station therefore
// starts in factory configuration (§11.3), and the three of them used to be left to the
// administration screen — on a station whose screen is exactly what the volunteer has to
// reach through a recovery code they may have already filed away.
//
// `password` is here for the same reason and says it in its own comment: what has no
// screen to be set from has to have a terminal. This action carries no secret at all,
// which is why its values may travel as arguments; the password next door reads the one
// secret of this command off the standard input, and nothing in `config` ever takes one
// from argv — a command line is readable in the process list by ANY user of the machine.
//
// It writes through editConfigFile like the three others: the store rotates config.json.1
// … .5, the file lands atomically (§11.4), and the station does not see the change before
// it restarts or is asked to re-read its file.

// stationEdit is what one `openscale config station` was asked to change.
//
// Number and Name are POINTERS because "set this field" and "leave it alone" are two
// different orders, and a zero value would be the first one said badly: 0 is a station
// number the controls refuse, and an empty name is what the delivered file already
// carries.
type stationEdit struct {
	Number *int
	Name   *string
	// DisableScale declares that this station has no scale — the explicit declaration of
	// §11.2, and NOT a way of naming one. Turning a scale back on means naming the port it
	// answers on, which is what the automatic detection of the administration screen is
	// for; a terminal has no port to offer, so this switch only goes one way.
	DisableScale bool
}

// setStationIdentity is `openscale config station` (§15.5, §11.2).
//
// It refuses to write a value the station's own controls would refuse. Without that, the
// command would trade one fault for another and report success: an operator who typed 0
// for their station number would read « c'est fait » and find out at the next restart,
// from a station that came up in factory configuration for a different reason than before.
func setStationIdentity(out io.Writer, path string, edit stationEdit) error {
	if edit.Number == nil && edit.Name == nil && !edit.DisableScale {
		return errors.New("config station ne change rien sans --number, --name ou --no-scale")
	}
	if edit.Name != nil && strings.TrimSpace(*edit.Name) == "" {
		return errors.New("--name est vide : c'est déjà ce que porte la configuration livrée, " +
			"et un poste sans nom ne se distingue de ses voisins que par son numéro")
	}

	var (
		posed         []string
		withoutManual bool
	)
	err := editConfigFile(path, func(cfg *domain.Config) error {
		if edit.Number != nil {
			cfg.Station.Number = *edit.Number
			posed = append(posed, fmt.Sprintf("numéro %d", *edit.Number))
		}
		if edit.Name != nil {
			cfg.Station.Name = strings.TrimSpace(*edit.Name)
			posed = append(posed, fmt.Sprintf("nom « %s »", cfg.Station.Name))
		}
		if edit.DisableScale {
			// THE PROTOCOL GOES WITH THE DECLARATION, and lowering `present` alone would not
			// do: control 3 refuses a protocol named by a station that declares no scale, and
			// control 6 goes on demanding whatever options that protocol's driver declares —
			// the serial port among them. Naming no protocol is what §11.2 calls the explicit
			// declaration of a station without a scale, and it is what takes
			// `scale.options.port` out of the fault list of a station straight out of the
			// installer.
			//
			// THE OPTIONS STAY, and that is where this parts company with the neutral profile
			// of §11.3, which carries none. Baud, bits, parity, stop and the reconnection
			// backoff are settings the FOUR STATIONS SHARE — they travel in the cloned
			// configuration and they count in the fingerprint of §15.5. Clearing them would
			// move this station away from its neighbours FOR GOOD: the automatic detection
			// that puts the scale back writes a port, not a serial dialect, so nothing would
			// bring those five values back. Measured on the delivered file: kept, the
			// fingerprint returns to the fleet's the moment the scale is declared again;
			// cleared, it lands somewhere else and stays there. A port left behind in a block
			// whose `present` is false opens nothing — no driver is built from it — and the
			// detection overwrites it.
			cfg.Scale.Present = false
			cfg.Scale.Type = ""
			withoutManual = !cfg.Scale.ManualEntryAllowed
			posed = append(posed, "balance déclarée absente")
		}
		return refuseWhatTheStationWouldRefuse(*cfg, edit)
	})
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "%s : %s.\n", path, strings.Join(posed, ", "))
	if edit.DisableScale {
		fmt.Fprintf(out, "La balance se remet en service depuis l'écran d'administration, page "+
			"« Matériel », « Détecter automatiquement », puis « Utiliser cette balance » sur le "+
			"port qui a répondu : ce bouton déclare la présence, le protocole et le port d'un "+
			"seul geste. Une ligne de commande, elle, n'a aucun port à nommer.\n")
		if withoutManual {
			fmt.Fprintf(out, "ATTENTION : ce poste n'autorise pas la saisie du poids à la main "+
				"(scale.manual_entry_allowed). Sans balance, il ne peut donc plus rien peser.\n")
		}
	}
	fmt.Fprintf(out, "Redémarrez le service pour que le poste le prenne en compte.\n")
	return nil
}

// refuseWhatTheStationWouldRefuse suspends the write when a field this command just set
// carries a fault.
//
// It judges the fields this run TOUCHED and no others, which is what makes it usable on a
// station that is still incomplete elsewhere — the ordinary case, since the installer
// calls this action on a file that has no printer queue and no catalogue address yet.
// Refusing on somebody else's fault would make posing a station number impossible until
// everything else was done, in the exact order nobody follows.
//
// The bounds are NOWHERE in this file: Config.Validate owns control 1, and a second place
// that spelled « [1, 99] » would be the place that disagrees the day the fleet grows.
func refuseWhatTheStationWouldRefuse(cfg domain.Config, edit stationEdit) error {
	var touched []string
	if edit.Number != nil {
		touched = append(touched, "station.number")
	}
	if edit.DisableScale {
		touched = append(touched, "scale.")
	}
	if len(touched) == 0 {
		return nil
	}

	var refused []string
	for _, fault := range cfg.Validate(registries()) {
		for _, field := range touched {
			if strings.HasPrefix(fault.Field, field) {
				refused = append(refused, fault.String())
				break
			}
		}
	}
	if len(refused) == 0 {
		return nil
	}
	return &serviceFailure{Exit: exitFailure, Message: fmt.Sprintf(
		"le fichier n'est pas modifié : %s", strings.Join(refused, " ; "))}
}
