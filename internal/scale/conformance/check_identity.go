package conformance

import (
	"strings"
	"testing"
	"unicode"

	"openscale/internal/fake"
)

// IDENTITY — clause 1. Descriptor is a REGISTRY KEY: an empty ID, a blank or an
// upper-case letter in it, and scale.type can no longer name the driver in config.json.

// checkDescriptor verifies the identity the registry and the admin form both read.
//
// Descriptor is called before anything is started, because that is when the Hub calls
// it: the drop-down list of scale.type is built from drivers nobody has opened yet.
func checkDescriptor(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	scale := build(t, r, subject.New, fake.NewClock(t0))
	defer closeAndForget(scale)

	descriptor := scale.Descriptor()
	if descriptor.ID == "" {
		r.Errorf("Descriptor().ID is empty. It is the key of the driver registry and the value of scale.type in config.json: an anonymous driver cannot be named by a configuration file, and the admin screen has nothing to generate its form from")
	}
	if descriptor.Label == "" {
		r.Errorf("Descriptor().Label is empty. It is what a volunteer replacing the hardware looks for in the menu, and it has to be the name printed on the device: « GRAM XFOC RS »")
	}
	if descriptor.NominalRate <= 0 {
		r.Errorf("Descriptor().NominalRate = %s, want > 0. The rate meter starts from the declared cadence and only leaves it once it holds eight intervals of its own; a zero cadence makes the derived expiry meaningless (§6.5)", descriptor.NominalRate)
	}
	if i := strings.IndexFunc(descriptor.ID, unicode.IsSpace); i >= 0 {
		r.Errorf("Descriptor().ID = %q holds a blank at byte %d. It is a configuration value a human types: nobody types a trailing space twice the same way, and the registry lookup is an exact string comparison", descriptor.ID, i)
	}
	if i := strings.IndexFunc(descriptor.ID, unicode.IsUpper); i >= 0 {
		r.Errorf("Descriptor().ID = %q holds an upper-case letter at byte %d. The registry is keyed on the exact string, so %q would be a different driver — the same trap the legacy application fell into with the case of its frame suffix", descriptor.ID, i, strings.ToLower(descriptor.ID))
	}
	if again := scale.Descriptor(); again != descriptor {
		r.Errorf("Descriptor() answered %+v then %+v. The registry, the admin form and the journal each read it separately; an identity that moves between two calls is not an identity", descriptor, again)
	}
}
