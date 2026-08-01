package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
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

// CurrentSchemaVersion is the shape of config.json this binary speaks.
//
// It is BOOKKEEPING and not an authority, and the difference matters. The field existed
// from the start (config.go:141) and nobody ever read it: only NeutralProfile set it, to 1.
// Every file in the field therefore announces 1 whatever its age, so a chain driven by this
// number could do nothing for any of them. The steps are driven by the KEYS PRESENT, are
// idempotent, and this number is written on the way out so that the next binary has a fast
// path and a volunteer has something to compare.
//
// 2 and not 1: the shape changed with ADR-034 and ADR-057, and a file this binary has been
// through has to be distinguishable from one it has not.
const CurrentSchemaVersion = 2

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
	carryCoefficientToDiscount,
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
	notes = append(notes, stampSchemaVersion(decoded)...)

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

// carryCoefficientToDiscount turns the rational coefficient of the tiers written before
// ADR-034 into the percentage that replaced it.
//
// The keys are PER TIER and never global -- PriceTier.CoefNum and CoefDen, up to cc3c604 --
// so a station running v0.1 to v0.3 carries one pair per line of its price grid.
//
// It converts EXACTLY OR NOT AT ALL. A discount is written to the tenth of a point
// (pricing.go:15), so 2/3 has no exact form, and rounding a cooperative's discount without
// telling it is the very thing ADR-034 refuses. What cannot be written exactly is refused,
// the two numbers stay in the document, and control 20 says so with the sentence it already
// has.
//
// A tier at coef 1/1 comes out with NO KEY AT ALL rather than a zero: ADR-034 holds that
// the absence of discount_percent IS the statement "this tier carries the catalogue price",
// which is also why the field is omitempty.
func carryCoefficientToDiscount(document map[string]any) []MigrationNote {
	pricing, ok := document["pricing"].(map[string]any)
	if !ok {
		return nil
	}
	tiers, ok := pricing["tiers"].([]any)
	if !ok {
		return nil
	}
	var notes []MigrationNote
	for index, raw := range tiers {
		tier, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if _, present := tier["coef_num"]; !present {
			if _, second := tier["coef_den"]; !second {
				continue
			}
		}
		path := fmt.Sprintf("pricing.tiers[%d].coef_num", index)
		numerator, haveNumerator := wholeNumber(tier["coef_num"])
		denominator, haveDenominator := wholeNumber(tier["coef_den"])

		switch {
		// Go does not panic on a signed integer overflow, it wraps in silence: past this
		// bound, (denominator-numerator)*int64(FullDiscount) below can land anywhere in the
		// int64 range, including a value the modulo test reads as exact and the division
		// after it turns into a discount inside [0, FullDiscount] -- one that control 13
		// would accept even though no file declared it. The bound is on the DENOMINATOR
		// alone, not on the size of the fraction: 1000/2000 stays a perfectly legal 50 %.
		case denominator > math.MaxInt64/int64(FullDiscount):
			notes = append(notes, MigrationNote{
				Key: path, Action: MigrationRefused,
				Message: fmt.Sprintf("le dénominateur %d est trop grand pour être converti "+
					"sans déborder le calcul : écrivez la remise de ce tarif en "+
					"pourcentage, au dixième de point (discount_percent, ADR-034)",
					denominator),
			})
		case !haveNumerator || !haveDenominator || denominator <= 0 || numerator < 0 ||
			numerator > denominator:
			notes = append(notes, MigrationNote{
				Key: path, Action: MigrationRefused,
				Message: fmt.Sprintf("le coefficient %v/%v n'est pas une fraction du prix "+
					"catalogue : écrivez la remise de ce tarif en pourcentage, au dixième "+
					"de point (discount_percent, ADR-034)",
					tier["coef_num"], tier["coef_den"]),
			})
		case (denominator-numerator)*int64(FullDiscount)%denominator != 0:
			notes = append(notes, MigrationNote{
				Key: path, Action: MigrationRefused,
				Message: fmt.Sprintf("le coefficient %d/%d ne s'écrit pas au dixième de "+
					"point : choisissez la remise voulue et écrivez-la en pourcentage "+
					"(discount_percent, ADR-034)", numerator, denominator),
			})
		default:
			discount := Discount((denominator - numerator) * int64(FullDiscount) / denominator)
			delete(tier, "coef_num")
			delete(tier, "coef_den")
			// The zero discount writes NO key: absence is the statement (ADR-034).
			if discount != 0 {
				tier["discount_percent"] = json.Number(discount.JSONText())
			}
			notes = append(notes, MigrationNote{
				Key: path, Action: MigrationCarried,
				Message: fmt.Sprintf("le coefficient %d/%d devient une remise de %s %% "+
					"(ADR-034)", numerator, denominator, discount),
			})
		}
	}
	return notes
}

// stampSchemaVersion writes the version this binary produced, and reports the ONE case it
// will not touch.
//
// A file stamped HIGHER than this binary speaks comes from a station whose binary was
// rolled back -- update.ps1 and update.sh both do that on their own when a station does not
// answer. Lowering the number would erase the trace of it, and refusing outright would put
// that station on the floor over a number. So it is left alone, with a note that says why.
func stampSchemaVersion(document map[string]any) []MigrationNote {
	declared, ok := wholeNumber(document["version"])
	if ok && declared > CurrentSchemaVersion {
		return []MigrationNote{{
			Key: "version", Action: MigrationRefused,
			Message: fmt.Sprintf("ce fichier a été écrit par une version plus récente "+
				"(schéma %d, ce binaire parle le %d) : il est lu tel quel, et rien n'y est "+
				"changé", declared, CurrentSchemaVersion),
		}}
	}
	document["version"] = json.Number(fmt.Sprint(CurrentSchemaVersion))
	return nil
}

// wholeNumber reports a decoded JSON value as a whole number, and whether it really was
// one. A quoted numeric literal is REFUSED, for the reason jsonNumber gives in config.go:
// a configuration that spells a number as text has a type error, and hiding it here would
// turn a wrong file into a silently wrong price.
func wholeNumber(value any) (int64, bool) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	whole, err := number.Int64()
	if err != nil {
		return 0, false
	}
	return whole, true
}
