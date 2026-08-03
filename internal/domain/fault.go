package domain

// This file holds what EVERY validation of this package returns, and the one
// predicate its lists of admissible values are read with.
//
// It is not the business of the template, of the configuration or of the numbering
// plan: the three of them answer in the same shape, so a screen renders one list of
// faults whatever produced it.

// Fault is a single validation error, named by the field that carries it.
//
// Validation returns ALL the faults, not the first one: the admin screen is used
// by volunteers, it must report everything at once, in French, with the offending
// field named and, whenever possible, the list of acceptable values.
type Fault struct {
	Field   string   `json:"field"`
	Message string   `json:"message"`
	Values  []string `json:"values,omitempty"`
}

func (f Fault) String() string { return f.Field + " : " + f.Message }

// known reports whether a value belongs to a closed list.
//
// It is the test behind every Fault.Values there is: what a fault offers as
// admissible and what the control accepts have to be the same list, read the same
// way, or a screen would propose a value its own station refuses.
func known(list []string, value string) bool {
	for _, candidate := range list {
		if candidate == value {
			return true
		}
	}
	return false
}
