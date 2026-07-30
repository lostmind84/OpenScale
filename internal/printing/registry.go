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
	// four destinations.
	//
	// It is NIL for a driver that declares no `transport` option, which is what `preview`
	// is: it writes files and opens nothing. The composition root builds a transport only
	// for the drivers whose own schema asks for one.
	Transport ports.Transport
	// OutputDir is where a driver that PRODUCES FILES writes them.
	//
	// It comes from the composition root for the same reason the transport does: a driver
	// never picks a path of its own, because the data directory is a fact about the
	// station and not about the driver.
	//
	// IT IS ALWAYS FILLED, and that is the difference with Transport — which is built only
	// for a driver whose own schema names the `transport` key. The root has no schema to
	// consult here: a directory costs nothing to hand over and opens nothing, where a
	// transport opens a device that would then have to be closed. A driver that produces no
	// file simply ignores it.
	//
	// This comment said the opposite until an agent writing a driver from the guide went
	// looking for the condition and found none (cmd/openscale/serve.go). The field a
	// file-producing driver depends on is exactly the one whose contract must not be
	// guessed.
	OutputDir string
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
	// SelfTests are the patterns of §8.6 this driver HONOURS — the ones that really
	// produce something when a volunteer presses their button.
	//
	// DECLARED HERE, AND NOT REFUSED AT PRINT TIME. That is ADR-025 applied to a screen:
	// a control exists only where a legitimate choice exists, and a button whose only
	// possible answer is a refusal is not a choice. `preview` used to answer a
	// well-written French sentence to `alignment` and `ruler` while the Matériel page went
	// on offering all three buttons — two of which failed on the click, in front of
	// somebody who was already looking for why nothing comes out. A declaration is what
	// lets the screen not draw them at all.
	//
	// What travels here is WHICH ONES, never the list itself: the three names are the
	// vocabulary of a screen and stay in SelfTests(). Register refuses a fourth.
	//
	// AN EMPTY LIST IS AN ASSERTION and not an omission — « this driver prints no
	// self-test at all » — and it is a legitimate thing for a driver to say. What no
	// driver may do is stay silent about a pattern it does not produce, because silence
	// is what put the two failing buttons on the screen.
	SelfTests []SelfTest
	// New builds an instance of this driver.
	New Factory
}

// selfTestNames reports the declaration as the descriptor and the screen carry it.
//
// A COPY, and plain strings: the value crosses into internal/domain, where the catalogue
// of §8.6 has no business being spelled a second time.
func (d Driver) selfTestNames() []string {
	if len(d.SelfTests) == 0 {
		return nil
	}
	out := make([]string, 0, len(d.SelfTests))
	for _, what := range d.SelfTests {
		out = append(out, string(what))
	}
	return out
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
	seen := map[SelfTest]bool{}
	for _, what := range d.SelfTests {
		switch {
		case !SelfTestExists(what):
			panic("printing: driver " + d.Descriptor.ID + " declares the self-test " +
				string(what) + ", which the catalogue of §8.6 does not carry; a name no screen " +
				"has a button for is a self-test nobody can launch")
		case seen[what]:
			panic("printing: driver " + d.Descriptor.ID + " declares the self-test " +
				string(what) + " twice")
		}
		seen[what] = true
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
			// The declaration of §8.6 travels with the descriptor for the reason the head
			// geometry does: the administration screen reads THIS, and a screen that had to
			// hold the list itself would offer a volunteer buttons the driver in service
			// answers with a refusal (ADR-025).
			SelfTests: driver.selfTestNames(),
			// The head geometry travels with the descriptor because controls 29 and 38
			// measure a template against it. Dropped here, the core had to hold the
			// figures of the WS408 as constants of its own, and every station with
			// another head failed its validation at start-up.
			Capabilities: driver.Descriptor.Capabilities,
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
