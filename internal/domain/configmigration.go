package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// This file owns what happens to a config.json written by an EARLIER version of this
// application.
//
// It exists because of one morning: a station updated on 01/08/2026 kept its file, the
// file carried ui.tile_size -- retired the same day by ADR-057 -- and the station came up
// on the neutral profile with ERR-CFG-01. The repair was manual, and nothing in the binary
// knew how to remove a key from a file already sitting on a station.
//
// Nothing here reads a clock, opens a file or a socket, exactly like config.go beside it:
// Migrate is pure, and what it cannot decide it REFUSES by doing nothing, which is what
// leaves control 20 free to speak the sentence it already speaks.

// MigrationAction is what this binary does with a key a file still carries.
type MigrationAction uint8

const (
	// MigrationCarried means the value was moved to its successor: pricing.tiers[i].coef_num
	// and coef_den become discount_percent. Nothing is lost, and the note says what became
	// what.
	MigrationCarried MigrationAction = iota
	// MigrationDropped means the key was removed because the replacement's DEFAULT is the
	// behaviour the key used to ask for. ui.tile_size is the case: grid_columns at 0 is the
	// grid ADR-035 already draws on those stations.
	MigrationDropped
	// MigrationRefused means this binary will not guess. The key STAYS IN THE DOCUMENT, so
	// control 20 finds it at decode and produces its fault word for word -- a migration must
	// never be able to hide a refusal.
	MigrationRefused
)

// String reports the action the way a note names it, in French.
func (a MigrationAction) String() string {
	switch a {
	case MigrationCarried:
		return "portée"
	case MigrationDropped:
		return "retirée"
	case MigrationRefused:
		return "refusée"
	}
	return "inconnue"
}

// MigrationNote is one thing this binary had to do to a configuration document, in the
// French an operator reads on the console and in diagnostic.zip.
type MigrationNote struct {
	// Key is the dotted path as it was FOUND, indices included: "pricing.tiers[1].coef_num".
	Key    string
	Action MigrationAction
	// Message says what happened and why, in French, naming the values on both sides when
	// there are two.
	Message string
}

// String writes the note the way the console shows it.
func (n MigrationNote) String() string {
	return fmt.Sprintf("%s : %s — %s", n.Key, n.Action, n.Message)
}

// retiredVerdicts says, for EVERY key of retiredKeys, what a file still carrying it gets.
//
// The two maps are required to have the same keys by
// TestEveryRetiredKeyHasADeclaredVerdict, and that test is the point of this whole file:
// retiring a key without saying what happens to the files already carrying it is what put
// a station out of service on 01/08/2026. Whoever retires the next one has to answer here.
//
// The six numbering-plan keys are MigrationRefused and it is not a shortcut: they entered
// the code already retired (8e434fa, 25/07/2026, lot L2 -- "le contrôle 20 ne refuse que
// les six clés du plan de numérotation"), so no released binary ever wrote one. There is
// no semantics to carry, and inventing one would be guessing at a file nobody has.
var retiredVerdicts = map[string]MigrationAction{
	"tile_size":         MigrationDropped,
	"coef_num":          MigrationCarried,
	"coef_den":          MigrationCarried,
	"weight_decimals":   MigrationRefused,
	"units_field_width": MigrationRefused,
	"weight_prefix":     MigrationRefused,
	"unit_prefix":       MigrationRefused,
	"content":           MigrationRefused,
	"rules_by_prefix":   MigrationRefused,
}

// migrationSteps are applied IN ORDER to the decoded document.
//
// A slice and not a map: two steps could one day touch the same block, and the order in
// which they do it would then depend on the iteration order of a map, which is random.
var migrationSteps = []func(document map[string]any) []MigrationNote{
	retireTileSize,
}

// Migrate brings a configuration DOCUMENT up to the schema this binary speaks, and reports
// everything it had to do.
//
// It works on the JSON document and NOT on a decoded Config, and that is the whole reason
// it exists: encoding/json refuses a field whose type changed, so a migration running after
// the decode would run on a station that already failed to start.
//
// The error is reserved for "this is not a JSON object at all". A document that decodes is
// always migrated -- what this binary cannot decide comes back as a refusal, never as an
// error, because an error at this point is a station that does not come up.
func Migrate(document []byte) ([]byte, []MigrationNote, error) {
	decoder := json.NewDecoder(bytes.NewReader(document))
	// UseNumber for the reason DriverOptions gives: decoding into `any` turns every number
	// into a float64, and no float carries a quantity in this application.
	decoder.UseNumber()
	var decoded map[string]any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, nil, fmt.Errorf("le document de configuration n'est pas un objet JSON : %w", err)
	}

	var notes []MigrationNote
	for _, step := range migrationSteps {
		notes = append(notes, step(decoded)...)
	}

	migrated, err := json.Marshal(decoded)
	if err != nil {
		return nil, nil, fmt.Errorf("le document migré n'a pas pu être réencodé : %w", err)
	}
	return migrated, notes, nil
}

// retireTileSize removes ui.tile_size (ADR-035, ADR-057).
//
// It removes and does NOT translate. small/medium/large was a DENSITY, that is a
// proportion, so one word lands on five, six or twelve columns depending on the screen --
// which is precisely why ADR-035 retired it and why ADR-057 did not bring it back.
// Writing a grid_columns here would reopen ADR-031 through the back door, and SUIVI.md of
// 01/08/2026 asks in as many words that it not be.
//
// What those stations get instead is grid_columns at 0, "automatic", which is the grid
// they have been drawing since v0.4 -- the key has been ignored at decode ever since.
func retireTileSize(document map[string]any) []MigrationNote {
	ui, ok := document["ui"].(map[string]any)
	if !ok {
		return nil
	}
	size, present := ui["tile_size"]
	if !present {
		return nil
	}
	delete(ui, "tile_size")
	return []MigrationNote{{
		Key:    "ui.tile_size",
		Action: MigrationDropped,
		Message: fmt.Sprintf("ce poste demandait des tuiles %v ; la grille automatique, "+
			"qui est le défaut de ui.grid_columns, est celle qu'il affiche depuis la "+
			"version 0.4 (ADR-035, ADR-057)", size),
	}}
}
