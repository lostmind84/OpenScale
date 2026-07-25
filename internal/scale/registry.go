// Package scale holds the weighing-device drivers and the registry the
// administration screen discovers them through.
//
// The registry is what makes the promise of §5.2 true: adding a balance is ONE
// PACKAGE plus ONE LINE in cmd/openscale/drivers.go, with zero modification to
// station, web or the front end. A driver registers its identity, its declared
// capabilities and the SCHEMA OF ITS OPTIONS; from that schema the administration
// screen generates the form, and Config.Validate checks the options of the chosen
// driver instead of merely saying "unknown type" (§11.3).
//
// What it deliberately cannot hold. scale.type names a HARDWARE PROTOCOL and
// nothing else (§9.3). The previous design mixed, in one drop-down shown to a
// volunteer, two protocols, a DEGRADED MODE (manual) and a TEST TOOL (replay) —
// which was Systeme.BalanceConnectee = O/N transposed. The same state was then
// reachable through three doors, a configuration value, an automatic fallback and a
// troubleshooting button, and the only question that matters on the morning of a
// breakdown became undecidable: WHY is this station in manual entry? The three
// questions are separate now — which protocol (scale.type), is there a scale
// (detected, or declared once by scale.present), can we weigh by hand
// (manual_entry_allowed) — and Register refuses the two names that are not
// protocols.
package scale

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// ErrUnknownDriver reports a scale.type no driver of this binary answers to.
var ErrUnknownDriver = errors.New("scale : ce scale.type ne correspond à aucun driver")

// Factory builds one driver instance from the options a configuration carries.
//
// The clock and the technical log are INJECTED and never created here: a driver that
// read the real clock would put its own reconnection out of reach of a test, and one
// that opened a log file would make a saturated journal degrade the service (§5.3,
// ADR-013).
type Factory func(o domain.DriverOptions, clk ports.Clock, log ports.TechnicalLog) (ports.Scale, error)

// Driver is one scale model as the registry knows it.
type Driver struct {
	// Descriptor is the identity of the model and the capabilities it honestly
	// declares. Its ID is the value that goes into scale.type, and its Label is the
	// name a volunteer reads on the hardware itself — a volunteer replacing a scale
	// must find « GRAM XFOC RS » in the menu, not a code (§9.3).
	Descriptor domain.ScaleDescriptor
	// Options is the schema the administration screen GENERATES its form from, and the
	// one Config.Validate checks scale.options against. A driver with no option at all
	// leaves it nil.
	Options []domain.OptionSchema
	// New builds an instance of this model.
	New Factory
}

// reservedIDs are the two values §9.3 keeps out of scale.type, each with the reason
// a panic will quote.
var reservedIDs = map[string]string{
	"manual": "manual entry is a STATE, entered automatically or from the troubleshooting " +
		"screen and always reversible, never a value somebody types into a file",
	"replay": "replaying frames is a DIAGNOSTIC TOOL — openscale capture / openscale replay, " +
		"the « Rejouer cette trame » button, the tests — and nobody, from a blank page, puts a " +
		"file reader in the enumeration of weighing hardware",
}

// Registry is the set of scale drivers this binary was built with.
//
// A value rather than a package-level map, and it costs nothing: registration
// happens once in the composition root, so the only thing a global would buy is a
// state shared between tests that cannot be reset. The composition root passes the
// descriptors to Config.Validate and the registry itself to whoever instantiates.
type Registry struct {
	drivers []Driver
}

// NewRegistry returns a registry with no driver in it.
func NewRegistry() *Registry { return &Registry{} }

// Register adds one driver. It is the ONE LINE of §5.2.
//
// It PANICS rather than returning an error, and that is deliberate. Every refusal is
// a composition mistake with no operator input anywhere in it — an empty ID, a
// driver registered twice, an ID that names a state instead of a protocol — so it is
// settled before the first weighing, exactly as an inconsistent numbering plan
// "stops the process at startup, never at print time" (§11.3, controls 17-18). An
// error here would have exactly one caller, in drivers.go, and that caller could
// only panic on it anyway.
//
// The messages are English on purpose: nobody but a developer can ever read them.
func (r *Registry) Register(d Driver) {
	switch {
	case d.Descriptor.ID == "":
		panic("scale: a driver registers under an ID, which is the value of scale.type")
	case d.Descriptor.Label == "":
		panic("scale: driver " + d.Descriptor.ID +
			" registers without the label a volunteer reads on the hardware (§9.3)")
	case d.New == nil:
		panic("scale: driver " + d.Descriptor.ID + " registers without a factory")
	}
	if why, reserved := reservedIDs[d.Descriptor.ID]; reserved {
		panic("scale: " + d.Descriptor.ID + " is not a hardware protocol and scale.type names " +
			"nothing else — " + why + " (§9.3)")
	}
	if _, exists := r.lookup(d.Descriptor.ID); exists {
		panic("scale: driver " + d.Descriptor.ID + " is registered twice")
	}
	r.drivers = append(r.drivers, d)
}

// Descriptors reports what the administration screen needs to build its drop-down
// list and the form behind each entry, in the order drivers.go registered them —
// which is therefore the order a volunteer reads.
//
// The result is a COPY, option schemas included: it is handed to a form generator
// and to Config.Validate, and a registry a caller can reach into is a registry that
// has stopped describing the binary.
func (r *Registry) Descriptors() []domain.DriverDescriptor {
	if len(r.drivers) == 0 {
		return nil
	}
	out := make([]domain.DriverDescriptor, 0, len(r.drivers))
	for _, driver := range r.drivers {
		out = append(out, domain.DriverDescriptor{
			ID:      driver.Descriptor.ID,
			Label:   driver.Descriptor.Label,
			Options: append([]domain.OptionSchema(nil), driver.Options...),
		})
	}
	return out
}

// New builds the driver scale.type names.
//
// It returns an error wrapping ErrUnknownDriver when no driver answers to id, and
// that error NAMES WHAT IS AVAILABLE: a configuration that spells a protocol wrong
// must produce the list of the ones that exist, never a bare "unknown type" (§11.3).
func (r *Registry) New(id string, o domain.DriverOptions, clk ports.Clock,
	log ports.TechnicalLog) (ports.Scale, error) {
	driver, ok := r.lookup(id)
	if !ok {
		return nil, fmt.Errorf("%w : %q ; %s", ErrUnknownDriver, id, r.availability())
	}
	return driver.New(o, clk, log)
}

// lookup finds a driver by the value of scale.type.
func (r *Registry) lookup(id string) (Driver, bool) {
	for _, driver := range r.drivers {
		if driver.Descriptor.ID == id {
			return driver, true
		}
	}
	return Driver{}, false
}

// availability is the French tail of the unknown-driver error. An empty registry
// says so instead of offering an empty list: on a binary built without drivers, "no
// protocol is available" is the fault, and a list of nothing hides it.
func (r *Registry) availability() string {
	if len(r.drivers) == 0 {
		return "aucun protocole n'est disponible dans ce binaire"
	}
	ids := make([]string, 0, len(r.drivers))
	for _, driver := range r.drivers {
		ids = append(ids, driver.Descriptor.ID)
	}
	sort.Strings(ids)
	return "protocoles disponibles : " + strings.Join(ids, ", ")
}
