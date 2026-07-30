package scale

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// stubScale is a ports.Scale that does nothing at all.
//
// The registry has no business knowing what a driver does, so the test that proves it
// registers, describes and instantiates uses the smallest thing that satisfies the
// contract — and importing a real driver package here would be the very coupling §5.2
// keeps in cmd/openscale/drivers.go.
type stubScale struct {
	descriptor domain.ScaleDescriptor
	options    domain.DriverOptions
	clock      ports.Clock
}

func (s *stubScale) Descriptor() domain.ScaleDescriptor { return s.descriptor }

func (s *stubScale) Start(_ context.Context, _ chan<- domain.ScaleEvent, done chan<- struct{}) error {
	close(done) // the mandatory corollary of §5.3, even here
	return nil
}

func (s *stubScale) Close() error { return nil }

// stubDecoder is a domain.Decoder that decodes nothing.
//
// The registry has no business knowing what a grammar recognises: what it must guarantee
// is that every entry HANDS ONE OVER, and a fresh one per call. It counts its own
// instances so that a test can prove the second is not the first.
type stubDecoder struct {
	fed []byte
}

func (d *stubDecoder) Feed(p []byte, _ time.Time) []domain.Measurement {
	d.fed = append(d.fed, p...)
	return nil
}

func (d *stubDecoder) Reset()              { d.fed = nil }
func (d *stubDecoder) FrameEnd([]byte) int { return -1 }
func (d *stubDecoder) Resyncs() int        { return 0 }

var _ domain.Decoder = (*stubDecoder)(nil)

// serialSchema is the shape of a serial link, as the model packages declare it.
var serialSchema = []domain.OptionSchema{
	{Key: "port", Kind: domain.OptionText, Required: true},
	{Key: "baud", Kind: domain.OptionInt},
}

// gramRS and gramPlus are the two entries of §9.3: two registry entries, one decoder.
func gramRS() Driver   { return stubDriver("gram-xfoc-rs", "GRAM XFOC RS") }
func gramPlus() Driver { return stubDriver("gram-xfoc-plus", "GRAM XFOC +") }

func stubDriver(id, label string) Driver {
	descriptor := domain.ScaleDescriptor{
		ID: id, Label: label, NominalRate: 400 * time.Millisecond,
		Capabilities: domain.Capabilities{Stability: true, Overload: true},
	}
	return Driver{
		Descriptor: descriptor,
		Options:    serialSchema,
		NewDecoder: func() domain.Decoder { return &stubDecoder{} },
		Endpoint:   EndpointSerialPort,
		New: func(o domain.DriverOptions, clk ports.Clock, _ ports.TechnicalLog) (ports.Scale, error) {
			return &stubScale{descriptor: descriptor, options: o, clock: clk}, nil
		},
	}
}

// requirePanic asserts that a composition mistake stops the binary, and that the
// message says which one.
func requirePanic(t *testing.T, mentions string, register func()) {
	t.Helper()
	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatalf("aucune panique, attendu un refus mentionnant %q", mentions)
		}
		message, ok := recovered.(string)
		if !ok || !strings.Contains(message, mentions) {
			t.Errorf("panique %v, elle doit mentionner %q", recovered, mentions)
		}
	}()
	register()
}

func TestTheAdminScreenDiscoversTheDriversByTheRegistry(t *testing.T) {
	// §5.2, and this is the whole point of the registry: the admin generates its
	// drop-down list and the form behind each entry from what a driver DECLARED, so
	// adding a scale modifies neither station, nor web, nor the front end.
	registry := NewRegistry()
	registry.Register(gramPlus())
	registry.Register(gramRS())

	descriptors := registry.Descriptors()
	if len(descriptors) != 2 {
		t.Fatalf("%d descripteurs, attendu 2", len(descriptors))
	}
	// Registration order is reading order: drivers.go decides what a volunteer sees
	// first, and nothing else does.
	if descriptors[0].ID != "gram-xfoc-plus" || descriptors[1].ID != "gram-xfoc-rs" {
		t.Errorf("ordre %s puis %s, attendu celui de l'enregistrement",
			descriptors[0].ID, descriptors[1].ID)
	}
	if descriptors[0].Label != "GRAM XFOC +" {
		t.Errorf("libellé %q : un bénévole qui remplace une balance doit retrouver dans le "+
			"menu le nom qu'il lit sur le matériel", descriptors[0].Label)
	}
	if len(descriptors[0].Options) != len(serialSchema) {
		t.Errorf("%d options déclarées, attendu %d — c'est le schéma qui génère le formulaire",
			len(descriptors[0].Options), len(serialSchema))
	}

	// And this is what Config.Validate consumes: exactly the protocols of the registry
	// (§11.3, controls 3-5).
	registries := domain.Registries{Scales: descriptors}
	types := registries.ScaleTypes()
	if len(types) != 2 || types[0] != "gram-xfoc-plus" || types[1] != "gram-xfoc-rs" {
		t.Errorf("scale.type disponibles %v, attendu les deux protocoles", types)
	}
}

func TestDescriptorsCannotBeUsedToRewriteTheRegistry(t *testing.T) {
	registry := NewRegistry()
	registry.Register(gramRS())

	stolen := registry.Descriptors()
	stolen[0].Label = "n'importe quoi"
	stolen[0].Options[0].Key = "n'importe quoi"

	fresh := registry.Descriptors()
	if fresh[0].Label != "GRAM XFOC RS" || fresh[0].Options[0].Key != "port" {
		t.Errorf("le registre a été réécrit par son appelant : %+v", fresh[0])
	}
	if serialSchema[0].Key != "port" {
		t.Fatalf("le schéma du driver a été réécrit : %+v", serialSchema[0])
	}
}

func TestNewBuildsTheDriverScaleTypeNames(t *testing.T) {
	registry := NewRegistry()
	registry.Register(gramPlus())

	options := domain.DriverOptions{}
	clock := stubClock{}
	built, err := registry.New("gram-xfoc-plus", options, clock, ports.NopTechnicalLog{})
	if err != nil {
		t.Fatalf("New : %v", err)
	}
	if got := built.Descriptor().ID; got != "gram-xfoc-plus" {
		t.Errorf("descripteur %q, attendu gram-xfoc-plus", got)
	}
	stub, ok := built.(*stubScale)
	if !ok {
		t.Fatalf("type %T, attendu le driver enregistré", built)
	}
	if stub.clock != ports.Clock(clock) {
		t.Error("l'horloge n'a pas été transmise : elle est INJECTÉE, jamais créée par un driver")
	}
}

func TestAnUnknownScaleTypeNamesWhatIsAvailable(t *testing.T) {
	registry := NewRegistry()
	registry.Register(gramRS())
	registry.Register(gramPlus())

	_, err := registry.New("gram-xfoc", nil, stubClock{}, nil)
	if !errors.Is(err, ErrUnknownDriver) {
		t.Fatalf("erreur %v, attendu %v", err, ErrUnknownDriver)
	}
	// A volunteer who mistyped a protocol must read the list of the ones that exist,
	// never a bare "unknown type" (§11.3).
	for _, want := range []string{"gram-xfoc-rs", "gram-xfoc-plus"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q, il doit nommer %q", err, want)
		}
	}
}

func TestAnEmptyRegistrySaysSoInsteadOfOfferingNothing(t *testing.T) {
	_, err := NewRegistry().New("gram-xfoc-rs", nil, stubClock{}, nil)
	if !errors.Is(err, ErrUnknownDriver) {
		t.Fatalf("erreur %v, attendu %v", err, ErrUnknownDriver)
	}
	if !strings.Contains(err.Error(), "aucun protocole") {
		t.Errorf("message %q : sur un binaire sans driver, c'est CELA la faute, et une "+
			"liste vide la masquerait", err)
	}
	if descriptors := NewRegistry().Descriptors(); descriptors != nil {
		t.Errorf("descripteurs %v, attendu aucun", descriptors)
	}
}

func TestScaleTypeNamesOnlyHardwareProtocols(t *testing.T) {
	// §9.3. The previous design put two protocols, a DEGRADED MODE and a TEST TOOL in
	// one drop-down shown to a volunteer, and the only question that matters on the
	// morning of a breakdown became undecidable: why is this station in manual entry?
	// The registry refuses the two names mechanically so that nobody has to remember.
	for _, id := range []string{"manual", "replay"} {
		t.Run(id, func(t *testing.T) {
			requirePanic(t, id, func() {
				NewRegistry().Register(stubDriver(id, "Un libellé"))
			})
		})
	}
}

func TestRegisterRefusesACompositionMistake(t *testing.T) {
	for _, tc := range []struct {
		name     string
		driver   Driver
		mentions string
	}{
		{"no ID", Driver{Descriptor: domain.ScaleDescriptor{Label: "Sans ID"},
			New: gramRS().New}, "an ID"},
		{"no label", Driver{Descriptor: domain.ScaleDescriptor{ID: "sans-libelle"},
			New: gramRS().New}, "label"},
		{"no factory", Driver{Descriptor: domain.ScaleDescriptor{
			ID: "sans-fabrique", Label: "Sans fabrique"}}, "factory"},
		// A driver with no decoder factory could be configured and never captured, never
		// detected and never replayed — and the omission would only surface on the morning
		// somebody needed a capture, as zero frames and no error.
		{"no decoder factory", Driver{
			Descriptor: domain.ScaleDescriptor{ID: "sans-decodeur", Label: "Sans décodeur"},
			New:        gramRS().New,
		}, "decoder factory"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requirePanic(t, tc.mentions, func() { NewRegistry().Register(tc.driver) })
		})
	}

	t.Run("registered twice", func(t *testing.T) {
		registry := NewRegistry()
		registry.Register(gramRS())
		requirePanic(t, "twice", func() { registry.Register(gramRS()) })
	})
}

// TestEveryDecoderIsItsOwn is the reason NewDecoder is a FACTORY and not a value.
//
// A decoder holds the bytes waiting for the rest of their frame. Two callers sharing one
// — two stations, or two entries of the same registry — means half a frame read on one
// port completed by the bytes of another, which is a mass nobody ever weighed on a label
// somebody sticks on a bag. The registry must be unable to hand the same one out twice.
func TestEveryDecoderIsItsOwn(t *testing.T) {
	registry := NewRegistry()
	registry.Register(gramRS())
	registry.Register(gramPlus())

	first, err := registry.NewDecoder("gram-xfoc-rs")
	if err != nil {
		t.Fatalf("NewDecoder : %v", err)
	}
	second, err := registry.NewDecoder("gram-xfoc-rs")
	if err != nil {
		t.Fatalf("NewDecoder : %v", err)
	}
	if first == second {
		t.Fatal("deux appels ont rendu LE MÊME décodeur : une demi-trame d'un port serait " +
			"complétée par les octets d'un autre")
	}

	// And the same holds across the entries of one detection: the two GRAM models share a
	// grammar, never a buffer.
	candidates := registry.Candidates(EndpointSerialPort)
	if len(candidates) != 2 {
		t.Fatalf("%d candidat(s), attendu les deux protocoles série", len(candidates))
	}
	if candidates[0].Decoder == candidates[1].Decoder {
		t.Fatal("les deux candidats d'une détection partagent un décodeur")
	}
}

// TestAnUnknownProtocolHasNoDecoderAndSaysWhichExist: a tool asked to decode a protocol
// this binary does not carry must refuse, never decode with another one.
func TestAnUnknownProtocolHasNoDecoderAndSaysWhichExist(t *testing.T) {
	registry := NewRegistry()
	registry.Register(gramRS())

	_, err := registry.NewDecoder("balance-inventee")
	if !errors.Is(err, ErrUnknownDriver) {
		t.Fatalf("erreur %v, attendu %v", err, ErrUnknownDriver)
	}
	if !strings.Contains(err.Error(), "gram-xfoc-rs") {
		t.Errorf("message %q, il doit nommer les protocoles disponibles", err)
	}
}

// TestADriverThatCannotBeDetectedIsNotACandidate is the legitimate declaration of §9.3: a
// protocol that only speaks when it is polled cannot be found by listening, and saying so
// is more useful than a detection whose only possible answer is silence.
func TestADriverThatCannotBeDetectedIsNotACandidate(t *testing.T) {
	silent := stubDriver("balance-muette", "Balance muette")
	silent.Endpoint = EndpointNone

	registry := NewRegistry()
	registry.Register(silent)
	registry.Register(gramRS())

	candidates := registry.Candidates(EndpointSerialPort)
	if len(candidates) != 1 || candidates[0].Descriptor.ID != "gram-xfoc-rs" {
		t.Fatalf("candidats %+v, attendu le seul protocole qui déclare un port série", candidates)
	}
	// And the declaration travels to whoever reads a descriptor instead of a registry:
	// `openscale doctor` asks it before checking « le port série ».
	descriptors := registry.Descriptors()
	if descriptors[0].Endpoint != domain.EndpointNone {
		t.Errorf("descripteur %q : endpoint %q, attendu %q",
			descriptors[0].ID, descriptors[0].Endpoint, domain.EndpointNone)
	}
	if descriptors[1].Endpoint != domain.EndpointSerialPort {
		t.Errorf("descripteur %q : endpoint %q, attendu %q",
			descriptors[1].ID, descriptors[1].Endpoint, domain.EndpointSerialPort)
	}
}

// stubClock is a clock nobody advances: the registry never reads it, it only has to
// hand it over.
type stubClock struct{}

func (stubClock) Now() time.Time { return time.Time{} }

func (stubClock) After(time.Duration) <-chan time.Time { return nil }

func (stubClock) Ticker(time.Duration) (<-chan time.Time, func()) { return nil, func() {} }
