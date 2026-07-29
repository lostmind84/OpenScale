//go:build !windows

package platform

// ApplyUpdate is not available off Windows.
//
// The station still carries the routes and answers honestly rather than hiding
// them: a Linux station that offered a button doing nothing would be worse than
// one that says the button does not exist here.
func ApplyUpdate(UpdateSpec) error { return ErrUpdateUnsupported }
