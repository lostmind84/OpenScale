package printing

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// ErrUnknownDriver reports a printer.type no driver of this binary answers to.
var ErrUnknownDriver = errors.New("printing : ce printer.type ne correspond à aucun driver")

// DefaultDriverID is the value printer.type carries when nobody has chosen: `raster`,
// the production path (§8.1, ADR-002).
//
// It is the constant of the domain and not a second spelling of the same word: the
// three values of printer.type are declared once, in internal/domain, because
// Config.Validate names them long before any driver is built.
const DefaultDriverID = domain.PrinterRaster

// DriverConfig is everything a printer driver is handed, and nothing it could invent.
//
// A struct rather than five positional parameters: the set already has five members and
// the day a driver needs a sixth, every existing factory would have to be re-typed for
// a value it ignores.
type DriverConfig struct {
	// Options is the printer.options block, as the driver's own schema describes it.
	Options domain.DriverOptions
	// Transport is the byte layer that carries the frame to the head (§8.4). It is
	// built by the composition root from printer.options.transport, because a printer
	// driver never opens a device itself — that is exactly what lets one frame reach
	// four destinations. The `preview` driver ignores it.
	Transport ports.Transport
	// Template is the label layout in service (printer.template).
	Template domain.Template
	// Clock times a job. time.Now is out of reach of every driver (§5.3).
	Clock ports.Clock
	// Log is where a driver reports what an operator may have to act on — a truncated
	// field, a missing glyph (§7.3). No driver opens a file.
	Log ports.TechnicalLog
	// DemoLabel supplies the label the `label` self-test prints (§8.6). It is injected
	// because a demonstration label carries a product and prices, which come from the
	// catalog and the configuration; a driver that made up a price would be inventing a
	// number nobody could check. Nil is legitimate — the self-test then refuses, in
	// French, naming what is missing.
	DemoLabel func() (domain.Label, error)
}

// Factory builds one printer driver from what a configuration and a composition root
// hand it.
type Factory func(c DriverConfig) (ports.Printer, error)

// Driver is one printer driver as the registry knows it.
type Driver struct {
	// Descriptor is the identity of the driver and the capabilities it honestly
	// declares. Its ID is the value that goes into printer.type, and its Label is what a
	// volunteer reads in the drop-down list.
	Descriptor domain.PrinterDescriptor
	// Options is the schema the administration screen GENERATES its form from, and the
	// one Config.Validate checks printer.options against (control 7). A driver with no
	// option at all leaves it nil.
	Options []domain.OptionSchema
	// New builds an instance of this driver.
	New Factory
}

// Registry is the set of printer drivers this binary was built with.
//
// A value rather than a package-level map, for the reason the scale registry gives:
// registration happens once in the composition root, so the only thing a global would
// buy is a state shared between tests that cannot be reset.
type Registry struct {
	drivers []Driver
}

// NewRegistry returns a registry with no driver in it.
func NewRegistry() *Registry { return &Registry{} }

// Register adds one driver. It is the ONE LINE of §5.2.
//
// It PANICS rather than returning an error, and that is deliberate. Every refusal here
// is a COMPOSITION mistake with no operator input anywhere in it — an empty ID, a
// driver registered twice, a nil factory — so it is settled before the first weighing,
// exactly as an inconsistent numbering plan « stops the process at startup, never at
// print time » (§11.3). An error would have exactly one caller, in drivers.go, and that
// caller could only panic on it anyway.
//
// The messages are English on purpose: nobody but a developer can ever read them.
func (r *Registry) Register(d Driver) {
	switch {
	case d.Descriptor.ID == "":
		panic("printing: a driver registers under an ID, which is the value of printer.type")
	case d.Descriptor.Label == "":
		panic("printing: driver " + d.Descriptor.ID +
			" registers without the label a volunteer reads in the drop-down list (§8.2)")
	case d.New == nil:
		panic("printing: driver " + d.Descriptor.ID + " registers without a factory")
	}
	if _, exists := r.lookup(d.Descriptor.ID); exists {
		panic("printing: driver " + d.Descriptor.ID + " is registered twice")
	}
	r.drivers = append(r.drivers, d)
}

// Descriptors reports what the administration screen needs to build its drop-down list
// and the form behind each entry, in the order drivers.go registered them — which is
// therefore the order a volunteer reads.
//
// The result is a COPY, option schemas included: it is handed to a form generator and
// to Config.Validate, and a registry a caller can reach into is a registry that has
// stopped describing the binary.
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

// New builds the driver printer.type names.
//
// It returns an error wrapping ErrUnknownDriver when no driver answers to id, and that
// error NAMES WHAT IS AVAILABLE: a configuration that spells a driver wrong must
// produce the list of the ones that exist, never a bare « type inconnu » (§11.3).
func (r *Registry) New(id string, c DriverConfig) (ports.Printer, error) {
	driver, ok := r.lookup(id)
	if !ok {
		return nil, fmt.Errorf("%w : %q ; %s", ErrUnknownDriver, id, r.availability())
	}
	return driver.New(c)
}

// lookup finds a driver by the value of printer.type.
func (r *Registry) lookup(id string) (Driver, bool) {
	for _, driver := range r.drivers {
		if driver.Descriptor.ID == id {
			return driver, true
		}
	}
	return Driver{}, false
}

// availability is the French tail of the unknown-driver error. An empty registry says
// so instead of offering an empty list: on a binary built without drivers, « no driver
// is available » is the fault, and a list of nothing hides it.
func (r *Registry) availability() string {
	if len(r.drivers) == 0 {
		return "aucun driver d'impression n'est disponible dans ce binaire"
	}
	ids := make([]string, 0, len(r.drivers))
	for _, driver := range r.drivers {
		ids = append(ids, driver.Descriptor.ID)
	}
	sort.Strings(ids)
	return "drivers disponibles : " + strings.Join(ids, ", ")
}
