package gramxfoc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"openscale/internal/domain"
	"openscale/internal/fake"
	"openscale/internal/scale"
	"openscale/internal/scale/serial"
	"openscale/internal/station/ports"
)

// t0 is where the injected clock starts. A fixed instant, because nothing here has any
// business reading the real one.
var t0 = time.Date(2026, 7, 25, 9, 30, 0, 0, time.UTC)

// watchdog is how long a test waits, on the wall clock, for something that should
// already have happened. It bounds a channel hand-off between two goroutines, never a
// decision of the driver: every delay the driver measures is on the injected clock.
const watchdog = 2 * time.Second

// portReadTimeout is what the fake port answers silence after.
//
// It is measured on the REAL clock, and it is the only thing here that is: it stands in
// for the timeout the operating system enforces inside a blocking serial read, which no
// injected clock can drive. It is short because a test has no scale to wait for.
const portReadTimeout = 20 * time.Millisecond

// --- the port -------------------------------------------------------------------

// port is the io.ReadCloser a test hands back instead of a serial port.
//
// Its read comes back with NO BYTE AND NO ERROR once its timeout elapses, which is what
// SetReadTimeout makes a real serial read do: a scale with nothing to say is not a
// broken link, and it is also what lets the loop notice a cancelled context.
type port struct {
	chunks chan []byte
	// opens carries the name of every port the loop asked for, so a test can assert
	// what reached the link without polling for it.
	opens chan string
}

func newPort() *port {
	return &port{chunks: make(chan []byte, 64), opens: make(chan string, 8)}
}

// open is the Opener injected in place of the real serial port.
func (p *port) open(o serial.Options) (io.ReadCloser, error) {
	select {
	case p.opens <- o.Port:
	default:
	}
	return p, nil
}

// deliver queues bytes for the next read, exactly as the wire would hand them over.
func (p *port) deliver(data string) { p.chunks <- []byte(data) }

func (p *port) Read(buffer []byte) (int, error) {
	select {
	case chunk := <-p.chunks:
		return copy(buffer, chunk), nil
	case <-time.After(portReadTimeout):
		return 0, nil
	}
}

func (p *port) Close() error { return nil }

// waitOpen returns the name of the next port the loop opened, verbatim.
func (p *port) waitOpen(t *testing.T) string {
	t.Helper()
	select {
	case name := <-p.opens:
		return name
	case <-time.After(watchdog):
		t.Fatal("la boucle n'a ouvert aucun port")
		return ""
	}
}

// --- the bench ------------------------------------------------------------------

// linkOptions is the scale.options block of a station, as config.json carries it.
func linkOptions() domain.DriverOptions {
	return domain.DriverOptions{"port": json.RawMessage(`"COM8"`)}
}

// startDriver builds one model, starts it on a fake port and guarantees it is stopped
// before the test returns: a leaked reader goroutine is a test that has stopped proving
// §13.1.
func startDriver(t *testing.T, id string, p *port) (ports.Scale, <-chan domain.ScaleEvent) {
	t.Helper()
	driver, err := newScale(Descriptor(id), linkOptions(), fake.NewClock(t0), nil, p.open)
	if err != nil {
		t.Fatalf("construction du driver %s : %v", id, err)
	}

	out := make(chan domain.ScaleEvent, 64)
	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	if err := driver.Start(ctx, out, done); err != nil {
		cancel()
		t.Fatalf("démarrage du driver %s : %v", id, err)
	}
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(watchdog):
			t.Error("la boucle n'a pas rendu la main : goroutine de lecture fuitée (§13.1)")
		}
		_ = driver.Close()
	})
	return driver, out
}

// nextMeasurement returns the mass of the next event that carries one, ignoring the
// status changes that travel on the same channel.
func nextMeasurement(t *testing.T, out <-chan domain.ScaleEvent) domain.Measurement {
	t.Helper()
	for {
		select {
		case event := <-out:
			if event.Measurement != nil {
				return *event.Measurement
			}
		case <-time.After(watchdog):
			t.Fatal("aucune mesure : le décodeur n'a rien rendu")
			return domain.Measurement{}
		}
	}
}

// noMeasurementWithin asserts that nothing comes out, which is what a half frame must
// produce.
func noMeasurementWithin(t *testing.T, out <-chan domain.ScaleEvent, patience time.Duration) {
	t.Helper()
	deadline := time.After(patience)
	for {
		select {
		case event := <-out:
			if event.Measurement != nil {
				t.Fatalf("mesure inattendue de %d g : une demi-trame ne s'invente pas",
					event.Measurement.Gross)
			}
		case <-deadline:
			return
		}
	}
}

// --- the two entries --------------------------------------------------------------

func TestTheTwoEntriesAreTheOnesAConfigurationNames(t *testing.T) {
	// The IDs are contract: they are the values of scale.type in config.json, and the
	// labels are what a volunteer reads on the hardware when a scale is replaced (§9.3).
	drivers := Drivers()
	if len(drivers) != 2 {
		t.Fatalf("%d entrées de registre, attendu 2 (§9.3)", len(drivers))
	}
	want := []domain.ScaleDescriptor{
		{ID: "gram-xfoc-rs", Label: "GRAM XFOC RS", NominalRate: 400 * time.Millisecond,
			Capabilities: domain.Capabilities{Stability: true, Overload: true}},
		{ID: "gram-xfoc-plus", Label: "GRAM XFOC +", NominalRate: 400 * time.Millisecond,
			Capabilities: domain.Capabilities{Stability: true, Overload: true}},
	}
	for i, driver := range drivers {
		if driver.Descriptor != want[i] {
			t.Errorf("entrée %d : descripteur %+v, attendu %+v", i, driver.Descriptor, want[i])
		}
		if len(driver.Options) != len(serial.OptionSchema()) {
			t.Errorf("entrée %s : %d options déclarées, attendu le schéma d'un lien série (%d)",
				driver.Descriptor.ID, len(driver.Options), len(serial.OptionSchema()))
		}
		if driver.New == nil {
			t.Errorf("entrée %s : aucune fabrique", driver.Descriptor.ID)
		}
	}
}

func TestTheTareIsNeverAnnouncedOnTheWire(t *testing.T) {
	// §19: the retare sequence of the legacy application was never once emitted in six
	// years. A driver that declared Tare would offer the engine a command no scale of
	// this parc answers.
	for _, id := range []string{IDRS, IDPlus} {
		if Descriptor(id).Capabilities.Tare {
			t.Errorf("%s déclare la tare sur la liaison : aucune balance du parc ne la prend", id)
		}
	}
}

func TestAnUnknownModelHasNoIdentity(t *testing.T) {
	if got := Descriptor("gram-xfoc"); got != (domain.ScaleDescriptor{}) {
		t.Errorf("descripteur %+v pour un modèle inconnu, attendu le descripteur nul", got)
	}
}

// --- the wiring -------------------------------------------------------------------

func TestTheLivingCorpusDecodesThroughTheDriver(t *testing.T) {
	// End to end, on the stride that broke the legacy application: CommRead(…, 18, …)
	// read EIGHTEEN FIXED BYTES per cycle for frames that are 18 bytes long including
	// their terminator, and one byte of drift cut every subsequent frame in two. Seven
	// frames go in, seven measurements come out.
	raw := readCorpus(t, "nominal-gram-xfoc.txt")
	p := newPort()
	_, out := startDriver(t, IDRS, p)

	for start := 0; start < len(raw); start += 18 {
		end := start + 18
		if end > len(raw) {
			end = len(raw)
		}
		p.deliver(string(raw[start:end]))
	}

	want := []domain.Grams{1236, 850, 1240, 1236, -282, 0, 99_999}
	for i, mass := range want {
		measurement := nextMeasurement(t, out)
		if measurement.Gross != mass {
			t.Errorf("trame %d : %d g, attendu %d g", i+1, measurement.Gross, mass)
		}
		if measurement.Timestamp != t0 {
			t.Errorf("trame %d : horodate %s, attendu celle de l'horloge injectée (%s)",
				i+1, measurement.Timestamp, t0)
		}
	}
}

func TestTheStabilityAndOverloadFlagsSurviveTheWiring(t *testing.T) {
	// The two flags the legacy application never read, and the reason the descriptor
	// declares the capability: OL is what safeguard rule 1 acts on (§6.4), and ST/US is
	// what the latch prefers to its own variation criterion (§6.5).
	p := newPort()
	_, out := startDriver(t, IDPlus, p)
	p.deliver("US,GS,+  1.240KG\r\nOL,GS,+ 99.999KG\r\n")

	unstable := nextMeasurement(t, out)
	if unstable.Stability != domain.Unstable {
		t.Errorf("stabilité %v, attendu Unstable", unstable.Stability)
	}
	overloaded := nextMeasurement(t, out)
	if !overloaded.Overload {
		t.Error("le drapeau OL n'est pas remonté : la règle 1 des garde-fous ne le verra pas (§6.4)")
	}
}

func TestEachInstanceGetsItsOwnAccumulator(t *testing.T) {
	// The one bug this wiring could plausibly have. A decoder shared between two
	// instances would let half a frame read on one port be completed by bytes read on
	// the other — a mass nobody weighed, which is the single class of error the grammar
	// exists to refuse. Here the two halves of "ST,GS,+  1.236KG" are split across two
	// drivers: a shared buffer would yield 1236 g, and nothing else can.
	first, second := newPort(), newPort()
	_, outFirst := startDriver(t, IDRS, first)
	_, outSecond := startDriver(t, IDPlus, second)

	first.deliver("ST,GS,+  1.2")
	second.deliver("36KG\r\n")

	if measurement := nextMeasurement(t, outSecond); measurement.Gross != 36_000 {
		t.Errorf("%d g sur le second driver, attendu 36000 g — 1236 g signerait un "+
			"accumulateur partagé entre deux instances", measurement.Gross)
	}
	noMeasurementWithin(t, outFirst, 5*portReadTimeout)
}

func TestThePortNameReachesTheLinkVerbatim(t *testing.T) {
	// Verbatim, because go.bug.st/serial is what prefixes \\.\ before calling
	// CreateFile: that is what makes "COM10" reachable, where the legacy application
	// built the path by hand and no station could be moved past COM9 (§9.1).
	p := newPort()
	startDriver(t, IDRS, p)

	if opened := p.waitOpen(t); opened != "COM8" {
		t.Errorf("port ouvert %q, attendu %q", opened, "COM8")
	}
}

// --- the registry -----------------------------------------------------------------

func TestTheRegistryBuildsBothEntries(t *testing.T) {
	registry := scale.NewRegistry()
	for _, driver := range Drivers() {
		registry.Register(driver)
	}

	descriptors := registry.Descriptors()
	if len(descriptors) != 2 {
		t.Fatalf("%d entrées dans le registre, attendu 2", len(descriptors))
	}
	if descriptors[0].Label != labelRS || descriptors[1].Label != labelPlus {
		t.Errorf("libellés %q et %q, attendus %q et %q — c'est ce qu'un bénévole lit sur "+
			"l'étiquette du matériel", descriptors[0].Label, descriptors[1].Label, labelRS, labelPlus)
	}

	for _, id := range []string{IDRS, IDPlus} {
		driver, err := registry.New(id, linkOptions(), fake.NewClock(t0), nil)
		if err != nil {
			t.Fatalf("%s : %v", id, err)
		}
		if got := driver.Descriptor().ID; got != id {
			t.Errorf("le registre a construit %q pour %q", got, id)
		}
		_ = driver.Close()
	}
}

func TestAMisspelledModelNamesTheOnesThatExist(t *testing.T) {
	// §11.3: a configuration that spells a protocol wrong must produce the list of the
	// ones that exist, never a bare "unknown type".
	registry := scale.NewRegistry()
	for _, driver := range Drivers() {
		registry.Register(driver)
	}
	_, err := registry.New("gram-xfoc", linkOptions(), fake.NewClock(t0), nil)
	if !errors.Is(err, scale.ErrUnknownDriver) {
		t.Fatalf("erreur %v, attendu ErrUnknownDriver", err)
	}
	for _, id := range []string{IDRS, IDPlus} {
		if !strings.Contains(err.Error(), id) {
			t.Errorf("le message ne nomme pas %s : %v", id, err)
		}
	}
}

func TestOptionsOfTheWrongTypeAreRefusedBeforeAnyPortIsOpened(t *testing.T) {
	// `"baud": "9600"` is a type error a volunteer has to be told about, not a baud rate
	// (§11.2). The factory refuses it, so nothing is ever started on it.
	driver, err := driverFor(Descriptor(IDRS)).New(
		domain.DriverOptions{"port": json.RawMessage(`"COM8"`), "baud": json.RawMessage(`"9600"`)},
		fake.NewClock(t0), nil)
	if err == nil {
		_ = driver.Close()
		t.Fatal("une vitesse entre guillemets a été acceptée")
	}
	if !strings.Contains(err.Error(), "baud") {
		t.Errorf("le message ne nomme pas la clé fautive : %v", err)
	}
}

// --- helpers ----------------------------------------------------------------------

// readCorpus reads one file of the living corpus, which lives under internal/scale so
// that `openscale capture` and the « Rejouer cette trame » button feed the same folder
// (§15.4).
func readCorpus(t *testing.T, name string) []byte {
	t.Helper()
	// The corpus is filed by protocol, and these captures are the RS ones: a capture is
	// read back by the grammar that produced it (internal/scale/corpus).
	raw, err := os.ReadFile(filepath.Join("..", "testdata", "frames", IDRS, name))
	if err != nil {
		t.Fatalf("lecture du corpus : %v", err)
	}
	return raw
}
