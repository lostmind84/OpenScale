package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// This file holds HOW THE FILE SPELLS the configuration: the codecs of the three
// domain types that predate config.json, and the one Config declares for itself.
//
// Their codecs live here rather than beside the types on purpose: safeguard.go says
// what a THRESHOLD is, quantity.go what a ROUNDING is, product.go what a CATEGORY
// is. How a file spells them is the business of the file, and the key names of §11.2
// then live in exactly one place.

// limitsJSON is the on-file shape of WeighingLimits.
type limitsJSON struct {
	EmptyMax           Grams `json:"empty_max_g"`
	BasketCheckEnabled bool  `json:"basket_check_enabled"`
	BasketMin          Grams `json:"basket_min_g"`
	BasketMax          Grams `json:"basket_max_g"`
	MinWeight          Grams `json:"min_weight_g"`
	MaxWeight          Grams `json:"max_weight_g"`
	MaxTare            Grams `json:"max_tare_g"`
	MinUnits           int   `json:"min_units"`
	MaxUnits           int   `json:"max_units"`
	MaxAmount          Cents `json:"max_amount_cents"`
}

// MarshalJSON writes the thresholds under the key names of §11.2.
func (l WeighingLimits) MarshalJSON() ([]byte, error) {
	return json.Marshal(limitsJSON{
		EmptyMax: l.EmptyMax, BasketCheckEnabled: l.BasketCheckEnabled,
		BasketMin: l.BasketMin, BasketMax: l.BasketMax,
		MinWeight: l.MinWeight, MaxWeight: l.MaxWeight, MaxTare: l.MaxTare,
		MinUnits: l.MinUnits, MaxUnits: l.MaxUnits, MaxAmount: l.MaxAmount,
	})
}

// UnmarshalJSON reads the thresholds, keeping whatever the block does not name.
//
// Keeping rather than zeroing is what makes the field-by-field merge of an import
// (§11.5) behave: a partial block overlays the target instead of erasing it.
func (l *WeighingLimits) UnmarshalJSON(raw []byte) error {
	on := limitsJSON{
		EmptyMax: l.EmptyMax, BasketCheckEnabled: l.BasketCheckEnabled,
		BasketMin: l.BasketMin, BasketMax: l.BasketMax,
		MinWeight: l.MinWeight, MaxWeight: l.MaxWeight, MaxTare: l.MaxTare,
		MinUnits: l.MinUnits, MaxUnits: l.MaxUnits, MaxAmount: l.MaxAmount,
	}
	if err := json.Unmarshal(raw, &on); err != nil {
		return err
	}
	*l = WeighingLimits{
		EmptyMax: on.EmptyMax, BasketCheckEnabled: on.BasketCheckEnabled,
		BasketMin: on.BasketMin, BasketMax: on.BasketMax,
		MinWeight: on.MinWeight, MaxWeight: on.MaxWeight, MaxTare: on.MaxTare,
		MinUnits: on.MinUnits, MaxUnits: on.MaxUnits, MaxAmount: on.MaxAmount,
	}
	return nil
}

// categoryJSON is the on-file shape of Category.
type categoryJSON struct {
	Code    string `json:"code"`
	Label   string `json:"label"`
	Rank    int    `json:"rank"`
	Color   string `json:"color"`
	Visible bool   `json:"visible"`
}

// MarshalJSON writes a category under the key names of §11.2.
func (c Category) MarshalJSON() ([]byte, error) {
	return json.Marshal(categoryJSON{c.Code, c.Label, c.Rank, c.Color, c.Visible})
}

// UnmarshalJSON reads a category, keeping whatever the object does not name.
func (c *Category) UnmarshalJSON(raw []byte) error {
	on := categoryJSON{c.Code, c.Label, c.Rank, c.Color, c.Visible}
	if err := json.Unmarshal(raw, &on); err != nil {
		return err
	}
	*c = Category{on.Code, on.Label, on.Rank, on.Color, on.Visible}
	return nil
}

// roundingSpellings maps the configuration wording of a policy to the policy.
var roundingSpellings = map[string]RoundingPolicy{
	"half_up":   RoundHalfUp,
	"truncate":  RoundTowardZero,
	"half_even": RoundHalfToEven,
}

// RoundingSpellings reports the three admissible spellings of a rounding policy,
// in a stable order, so that a fault and an admin drop-down list name the same
// three values.
func RoundingSpellings() []string { return []string{"half_up", "truncate", "half_even"} }

// MarshalJSON writes the policy as the word config.json uses.
func (p RoundingPolicy) MarshalJSON() ([]byte, error) { return json.Marshal(p.String()) }

// UnmarshalJSON reads one of the three words of RoundingSpellings.
//
// An unknown word is an ERROR and not a fault, so the configuration never holds a
// policy nobody declared: Divide would silently truncate, and a station would
// under-charge by a cent for months without anyone able to name why. §11.4 turns
// this into the 400 Bad Request of step 1, and the error names the three values.
func (p *RoundingPolicy) UnmarshalJSON(raw []byte) error {
	var word string
	if err := json.Unmarshal(raw, &word); err != nil {
		return fmt.Errorf("domain: un arrondi est un mot parmi %s : %w",
			strings.Join(RoundingSpellings(), ", "), err)
	}
	policy, ok := roundingSpellings[word]
	if !ok {
		return fmt.Errorf("domain: arrondi inconnu %q, valeurs admises : %s",
			word, strings.Join(RoundingSpellings(), ", "))
	}
	*p = policy
	return nil
}

// UnmarshalJSON reads the configuration and remembers the retired keys it carried.
//
// The scan happens HERE and not in Validate because Validate only sees a Go
// structure, in which a retired key cannot exist: encoding/json drops what no field
// claims. Control 20 has to refuse the FILE, so the file is what gets read.
func (c *Config) UnmarshalJSON(raw []byte) error {
	// The generic pass FIRST, so that "this is not JSON" and "this JSON puts a word
	// where a number belongs" are two distinct errors rather than one.
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var document any
	if err := decoder.Decode(&document); err != nil {
		return err
	}
	// An alias, otherwise this method calls itself.
	type alias Config
	shadow := alias(*c)
	if err := json.Unmarshal(raw, &shadow); err != nil {
		return err
	}
	*c = Config(shadow)
	// A file that names no repository -- one written before this block existed,
	// or one that carries it empty -- runs on the default. Refusing here would put
	// a station out of service over a field nobody meant to set.
	if c.Update.Repository == "" {
		c.Update.Repository = DefaultUpdateRepository
	}
	// Zero is not a threshold, it is a file that says nothing -- and the delivered file
	// is one of them, on purpose (§11.2). Refusing it here would make a station refuse
	// its own delivered configuration, which is the defect of 28/07/2026; correcting it
	// is what the block above already does for a repository nobody named.
	if c.UI.MinProductsForChip == 0 {
		c.UI.MinProductsForChip = DefaultMinProductsForChip
	}
	c.retired = nil
	scanRetired("", document, &c.retired)
	return nil
}
