package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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

// WholeDocumentField is the Fault.Field DecodeConfigBlockByBlock reports when the
// DOCUMENT itself failed to decode, as opposed to one block within it. Everywhere else a
// Fault names a dotted path into the configuration -- "pricing", "scale.options" -- and
// this is the one value that names no path at all: nothing below it decoded either.
//
// internal/diag derives loadedConfig.Parsed from it: the two cases carry two different
// remedies (§11.3), a bad block is fixed in place and a document that is not JSON at all
// is restored from config.json.1, and telling them apart by re-parsing the message would
// drift from what this function actually decided.
const WholeDocumentField = "config.json"

// UnreadableBlocksError reports that a configuration FILE was read and that PART of it did
// not decode: the Config it carries holds the neutral profile in place of those blocks.
//
// It exists because « je n'ai pas pu tout lire » has no single right answer -- the right
// answer depends on what the caller is about to DO, and there are three kinds:
//
//   - a caller that DISPLAYS the file must show what was read AND name what was
//     substituted, or it presents factory values as the shop's own;
//   - a caller that REWRITES THE FILE WHOLE must never write a substituted block back,
//     because that is exactly how a factory value nobody declared becomes the shop's own,
//     permanently, on the next read;
//   - a caller that needs ONE BLOCK must not be stopped by a fault in another.
//
// A plain error collapses the three into one, and doing so cost a defect in each
// direction on the same file. Tolerated silently (before 02/08/2026), the second kind
// wrote the factory grid over a cooperative's tariffs. Refused outright (02/08/2026), the
// first and third broke instead: the recovery route stopped reading the file at all and
// wrote the FOURTEEN factory blocks onto it -- identity, tariffs, catalog credentials,
// safeguards -- with HTTP 200 and no warning, on the one gesture that exists to rescue
// that station.
//
// So the verdict is not decided here. Callers interrogate it with errors.As and choose.
type UnreadableBlocksError struct {
	// Config is what DecodeConfigBlockByBlock produced: every block that decoded, and the
	// neutral profile in place of every block that did not.
	//
	// It is OFFERED rather than withheld because most callers have something right to do
	// with it -- and none of them can, without knowing which blocks not to trust, which is
	// what Faults names.
	Config Config
	// Faults name the blocks that did not decode, with the reason, in French.
	Faults []Fault
}

// Error names the blocks that did not decode.
func (e *UnreadableBlocksError) Error() string {
	return fmt.Sprintf("domain: config block(s) did not decode: %s", joinFields(e.Faults))
}

// Blocks reports the names of the blocks that did not decode, in the order they were
// found. A whole document that did not decode at all names WholeDocumentField, and that is
// a fourth answer again: there is no block to keep and nothing to display.
func (e *UnreadableBlocksError) Blocks() []string {
	out := make([]string, 0, len(e.Faults))
	for _, fault := range e.Faults {
		out = append(out, fault.Field)
	}
	return out
}

// Names reports whether one of the unreadable blocks is the one asked about. It is what
// lets a caller that needs a SINGLE block carry on when the fault is elsewhere.
func (e *UnreadableBlocksError) Names(block string) bool {
	for _, fault := range e.Faults {
		if fault.Field == block || fault.Field == WholeDocumentField {
			return true
		}
	}
	return false
}

// joinFields lists the fields of some faults, comma separated.
func joinFields(faults []Fault) string {
	out := make([]string, 0, len(faults))
	for _, fault := range faults {
		out = append(out, fault.Field)
	}
	return strings.Join(out, ", ")
}

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
	err := json.Unmarshal(document, &blocks)
	// `null` unmarshals into a map WITHOUT an error and leaves it nil, which would come out
	// of here as a station whose file has no fault at all while it runs on the factory
	// profile -- the same silence Migrate refuses one step earlier, and it has to be refused
	// on both doors or the quiet one wins.
	if err == nil && blocks == nil {
		err = errors.New("le document vaut null")
	}
	if err != nil {
		return cfg, []Fault{{
			Field: WholeDocumentField,
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
			Field:   WholeDocumentField,
			Message: fmt.Sprintf("les blocs lisibles n'ont pas pu être rassemblés (%s)", err),
		})
	}
	return cfg, faults
}
