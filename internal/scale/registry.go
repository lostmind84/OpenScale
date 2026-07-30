package scale

// The package comment lives in doc.go, which is where a contributor arriving in this
// tree looks first — and where the three gestures that add a scale are written.

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

// DecoderFactory builds ONE decoder of a protocol.
//
// A FACTORY AND NOT A VALUE, and the reason is the mass this grammar exists to refuse.
// A decoder holds the bytes waiting for the rest of their frame; two stations, or two
// entries of the same registry, sharing one buffer means half a frame read on one port
// completed by the bytes of another — a mass nobody ever weighed, on a label somebody
// sticks on a bag. Every caller that needs a decoder therefore asks for its own, and
// there is no way to hand out a shared one by mistake.
type DecoderFactory func() domain.Decoder

// Endpoint names the family of ACCESS POINTS a protocol is looked for on.
//
// The split it draws is the one §14.4 needs. ENUMERATING the access points of a machine
// — which serial ports does it have, which addresses answer on this subnet — is a
// question put to the OPERATING SYSTEM, and it stays in the composition root where the
// platform lives. RECOGNISING what answers is a question about the PROTOCOL, and that
// belongs to the driver, which is the only thing that knows what one of its frames looks
// like.
type Endpoint uint8

const (
	// EndpointNone declares a protocol « Détecter automatiquement » cannot look for,
	// and it is a LEGITIMATE declaration rather than an omission — a scale that only
	// speaks when it is polled, or one reached through a vendor library, is not
	// recognisable by listening.
	//
	// It is the zero value on purpose: a driver that says nothing says the thing that
	// cannot mislead. The screen then names the protocol and tells the volunteer to
	// choose it by hand, which is a sentence they can act on; a detection button that
	// answered silence sent them looking for a cable instead.
	EndpointNone Endpoint = iota
	// EndpointSerialPort is one serial port of this machine, as internal/platform
	// enumerates them.
	EndpointSerialPort
)

// String reports the endpoint the way a descriptor carries it, which is the ONE spelling
// of these words: `openscale doctor` and the administration screen read the descriptor,
// and a second spelling on the far side is how a declaration stops matching its reader.
func (e Endpoint) String() string {
	if e == EndpointSerialPort {
		return domain.EndpointSerialPort
	}
	return domain.EndpointNone
}

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
	// NewDecoder builds the decoder of THIS protocol, and it is what every tool that
	// reads bytes without running a station asks for: the detection of §14.4,
	// `openscale capture`, `openscale replay`, the « Rejouer cette trame » button.
	//
	// Those four built a frame.Accumulator in the composition root, which is to say they
	// spoke the grammar of the GRAM whatever scale.type said. None of them FAILED on
	// another protocol — they returned zero frames in silence, which is worse: it is the
	// answer a broken cable gives, and it sends a volunteer to check one.
	//
	// It is MANDATORY. A driver with no decoder could be configured and never captured,
	// never detected and never replayed, and the omission would only surface on the
	// morning somebody needed a capture.
	NewDecoder DecoderFactory
	// Endpoint declares where — and whether — this protocol can be recognised.
	//
	// EndpointNone is legitimate and says « choose me by hand ». See Endpoint.
	Endpoint Endpoint
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
	case d.NewDecoder == nil:
		panic("scale: driver " + d.Descriptor.ID + " registers without a decoder factory; " +
			"the detection, openscale capture, openscale replay and the « Rejouer cette trame » " +
			"button all read bytes without running a station, and a driver that names no grammar " +
			"leaves each of them decoding with somebody else's — silently, as zero frames")
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
			// The declaration of where this protocol lives travels with the descriptor,
			// because what reads it is on the other side of the composition root: control 10
			// of `openscale doctor` asks whether « le port série » is a question that even
			// applies to the protocol this station declares.
			Endpoint: driver.Endpoint.String(),
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

// NewDecoder builds a FRESH decoder of the protocol id names.
//
// It is what `openscale capture`, `openscale replay` and the « Rejouer cette trame »
// button ask for instead of building the grammar of one model in the composition root.
// The error wraps ErrUnknownDriver and names what is available, exactly as New does: a
// tool asked to decode a protocol this binary does not carry must say so, and never
// decode with another one.
func (r *Registry) NewDecoder(id string) (domain.Decoder, error) {
	driver, ok := r.lookup(id)
	if !ok {
		return nil, fmt.Errorf("%w : %q ; %s", ErrUnknownDriver, id, r.availability())
	}
	return driver.NewDecoder(), nil
}

// Candidate is one protocol a detection tries on an access point: what to call it, and
// a decoder of its own to try it with.
type Candidate struct {
	// Descriptor names the protocol the detection would propose, and carries the label
	// a volunteer reads on the hardware.
	Descriptor domain.ScaleDescriptor
	// Decoder is FRESH and belongs to this candidate alone. Two candidates sharing a
	// buffer would complete one another's half frames, which is the fabricated mass the
	// grammar exists to refuse.
	Decoder domain.Decoder
}

// Candidates returns one candidate per protocol that declares it can be recognised on
// e, in the order drivers.go registered them.
//
// An EMPTY result is an answer and not an accident: it means no protocol of this binary
// knows how to be recognised on that kind of access point, and the screen has to say so
// — « choisissez-le à la main » — rather than offer a detection whose only possible
// outcome is silence.
func (r *Registry) Candidates(e Endpoint) []Candidate {
	var out []Candidate
	for _, driver := range r.drivers {
		if driver.Endpoint != e {
			continue
		}
		out = append(out, Candidate{Descriptor: driver.Descriptor, Decoder: driver.NewDecoder()})
	}
	return out
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
