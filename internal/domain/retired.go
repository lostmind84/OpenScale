package domain

import (
	"fmt"
	"strings"
)

// This file holds the keys this binary REFUSES to read, and everything that names
// them: the reason each one left (§11.2), the scan that finds them in a file, and
// the two ways a caller is told about them.
//
// Refusing rather than ignoring is the whole point, and it is written key by key
// below: encoding/json drops what no field claims, so a configuration carrying an
// obsolete key would decode in SILENCE, with the fact it declared simply gone.

// retiredKeys are the keys control 20 REFUSES outright, each with the reason §11.2
// gives for its removal.
//
// Two families, and refusing rather than ignoring is the whole point of both.
// The first six used to declare a piece of the numbering plan from a file; the
// plan is now a CONSTANT OF THE BINARY indexed by prefix and self-checked at
// start-up (ADR-028), because a field that changes the MEANING of the code the
// till reads is not a setting, it is an external contract. The last two are the
// rational coefficient ADR-034 replaced by a percentage: encoding/json drops
// what no field claims, so an old file would decode in silence with every
// discount at zero -- and every member would pay the full price with nothing to
// say why.
var retiredKeys = map[string]string{
	"weight_decimals":   "les décimales du poids sont déclarées par le plan compilé, indexé par préfixe (ADR-028)",
	"units_field_width": "la largeur du champ des unités est déclarée par le plan compilé, indexé par préfixe (ADR-028)",
	"weight_prefix":     "les préfixes au poids sont déclarés par le plan compilé (0493 à 0498), jamais par un fichier",
	"unit_prefix":       "le préfixe à l'unité est déclaré par le plan compilé (0499), jamais par un fichier",
	"content":           "ce que transporte la charge utile est déclaré par le plan compilé, jamais par un fichier",
	"rules_by_prefix":   "la table de règles par préfixe est remplacée par le plan compilé, auto-contrôlé au démarrage",
	"coef_num":          "la remise d'un tarif se déclare en pourcentage : discount_percent, au dixième de point (ADR-034)",
	"coef_den":          "la remise d'un tarif se déclare en pourcentage : discount_percent, il n'y a plus de dénominateur (ADR-034)",
	"tile_size":         "la densité de la grille s'adapte en continu à l'écran (clamp CSS), il n'y a plus de palier à choisir (ADR-035, remplace ADR-031) ; ce qui se règle désormais est le nombre de colonnes, ui.grid_columns, un entier (ADR-057)",
}

// RetiredKeyReason reports why the key at the end of a dotted path -- "barcode.weight_decimals",
// exactly as scanRetired and Config.Retired name it -- was retired, in the French an
// operator reads.
//
// It exists so that a reason written ONCE in retiredKeys is read everywhere a refusal is
// shown, instead of being copied a second time by whoever writes the next one: control 20
// and `openscale config migrate` (cmd/openscale/config.go) both name a key this binary
// will not convert, and they have to say the SAME thing, word for word, or a volunteer
// comparing the two would read them as two different problems.
//
// The extraction is the one control 20 already did before this function existed: the last
// segment of the path, because retiredKeys is indexed by the bare key and not by where it
// was found. A path this binary never retired -- unreachable through Config.Retired, which
// only ever names a key of retiredKeys -- reports that plainly rather than an empty string,
// which would truncate whatever sentence names it.
func RetiredKeyReason(path string) string {
	key := path[strings.LastIndexByte(path, '.')+1:]
	if reason, known := retiredKeys[key]; known {
		return reason
	}
	return "clé retirée dont la raison n'est plus documentée"
}

// scanRetired appends the dotted path of every retired key of a decoded document.
func scanRetired(prefix string, value any, out *[]string) {
	switch typed := value.(type) {
	case map[string]any:
		for _, key := range sortedKeys(typed) {
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}
			if _, retired := retiredKeys[key]; retired {
				*out = append(*out, path)
			}
			scanRetired(path, typed[key], out)
		}
	case []any:
		for i, item := range typed {
			scanRetired(fmt.Sprintf("%s[%d]", prefix, i), item, out)
		}
	}
}

// Retired reports the dotted paths of the retired keys the file carried, in a
// stable order.
//
// It exists so that the administration screen can say « supprimez ces lignes »
// while pointing at the file, and so that a test can assert on the FILE rather than
// on a structure in which a retired key cannot exist.
func (c *Config) Retired() []string {
	return append([]string(nil), c.retired...)
}

// RetiredKeysError reports that a Config still carries a key control 20 refuses.
//
// It is what ConfigStore.Save returns instead of writing: the struct is about to be
// marshalled, and marshalling is what LAUNDERS the key -- encoding/json already
// dropped it once, at decode, and the field it stood for (a member's discount, for
// coef_num) goes with it. A caller that reaches Save without having checked first
// gets this instead of a file that decodes clean on the very next read.
type RetiredKeysError struct {
	// Keys are the dotted paths Config.Retired returned.
	Keys []string
}

// Error names the retired keys.
func (e *RetiredKeysError) Error() string {
	return fmt.Sprintf("domain: config still carries retired key(s): %s", strings.Join(e.Keys, ", "))
}

// RefuseIfRetired reports a *RetiredKeysError when the configuration still carries a
// key control 20 refuses, and nil otherwise.
//
// It is deliberately narrower than Validate: Validate needs Registries and can fail
// on a print queue this station does not have, which is not a reason to refuse
// WRITING a configuration that was already sitting on disk. This checks the one
// thing that must never reach a file regardless of everything else about it -- and
// it is cheap enough to run on every save, by every caller, including the ones that
// will never think to call Validate first (the recovery route does not: a rescue
// cannot be made to depend on the very validation that put the station out of
// service to begin with).
func (c *Config) RefuseIfRetired() error {
	if keys := c.Retired(); len(keys) > 0 {
		return &RetiredKeysError{Keys: keys}
	}
	return nil
}
