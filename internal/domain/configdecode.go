package domain

import (
	"encoding/json"
	"fmt"
)

// This file owns the ONE reading of config.json that is allowed to be tolerant.
//
// §11.3 says an invalid configuration never kills the process and a broken configuration
// never produces a black screen. That was true of a file that DECODES and then fails
// Validate, and false of every other kind: a single field whose type changed took the whole
// document down, and with it the station.
//
// Config.UnmarshalJSON is deliberately NOT tolerant and does not change: an unknown
// rounding word is an error there because PUT /admin/api/config turns it into the 400 of
// §11.4 step 1. Refusing what a human is typing and refusing what a station is booting on
// are two different jobs, and this file is the second one.

// DecodeConfigBlockByBlock decodes a configuration document, replacing every top-level
// block that will not decode with the one of the neutral profile, and reporting a fault
// that names it.
//
// It starts from NeutralProfile so that a dropped block keeps a USABLE value rather than a
// zero one -- a station serving its fault list must still be a station -- and it relies on
// Config.UnmarshalJSON keeping what a document does not name, which limitsJSON and
// categoryJSON already do on purpose for the field-by-field merge of §11.5.
//
// The faults it returns are DECODING faults. They join the ones Validate produces; nothing
// here judges a value.
func DecodeConfigBlockByBlock(document []byte) (Config, []Fault) {
	cfg := NeutralProfile()

	var blocks map[string]json.RawMessage
	if err := json.Unmarshal(document, &blocks); err != nil {
		return cfg, []Fault{{
			Field: "config.json",
			Message: fmt.Sprintf("le fichier n'est pas un document JSON exploitable (%s) : "+
				"le poste sert cet écran sur la configuration d'usine. Corrigez le fichier "+
				"— c'est presque toujours une virgule en trop avant une accolade — ou "+
				"restaurez config.json.1, la version d'avant", err),
		}}
	}

	// Each block is probed ALONE, against a fresh neutral profile, so that the one that
	// fails is named and the others are untouched. Sorted, so two runs of a broken file
	// report their faults in the same order.
	var faults []Fault
	keep := make(map[string]json.RawMessage, len(blocks))
	for _, name := range sortedKeys(blocks) {
		probe := NeutralProfile()
		alone, err := json.Marshal(map[string]json.RawMessage{name: blocks[name]})
		if err == nil {
			err = json.Unmarshal(alone, &probe)
		}
		if err != nil {
			faults = append(faults, Fault{
				Field: name,
				Message: fmt.Sprintf("ce bloc n'a pas pu être lu (%s) : le poste tourne sur "+
					"celui de la configuration d'usine, et le reste du fichier est intact", err),
			})
			continue
		}
		keep[name] = blocks[name]
	}

	// Everything that survived, in one pass, so the retired-key scan of Config.UnmarshalJSON
	// sees a whole document rather than fourteen fragments.
	filtered, err := json.Marshal(keep)
	if err == nil {
		err = json.Unmarshal(filtered, &cfg)
	}
	if err != nil {
		// Unreachable by construction -- every block decoded alone a moment ago -- and
		// reported rather than ignored, because "unreachable" is what a silent zero
		// configuration always looks like from the outside.
		faults = append(faults, Fault{
			Field:   "config.json",
			Message: fmt.Sprintf("les blocs lisibles n'ont pas pu être rassemblés (%s)", err),
		})
	}
	return cfg, faults
}
