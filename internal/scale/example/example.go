// Package example is a scale driver written to be COPIED, and registered nowhere.
//
// It is a complete ports.Scale: it declares an identity, the capabilities it honestly has,
// the option schema of its link, a decoder of its own and a factory, and it passes
// internal/scale/conformance unchanged. What is not real is the PROTOCOL — no scale speaks
// the nine bytes decoder.go documents — because a repository owns no hardware.
//
// # How to use it
//
// Copy the directory, rename the package, then follow the TODO(driver) markers: there is
// one on every point of variation and nowhere else. docs/07-ajouter-un-materiel.md walks
// the same path in French, with the commands, and it says the thing that matters most:
// CAPTURE THE STREAM BEFORE WRITING A LINE. The manual of a scale is a hypothesis until a
// capture confirms it, and the one manual this project trusted turned out to be wrong about
// the framing, about the status separator and about the checksum — the driver written from
// it decoded ZERO frames on the bench.
//
// # What the package really contains, and what it does not
//
// A model brings THREE things and no more: a descriptor, its link defaults, and a
// domain.Decoder. Everything else — opening the port, reading what has arrived, feeding the
// decoder, publishing, reconnecting with a backoff, closing without hanging — is
// internal/scale/serial, written once and shared. That is 95 % of a serial driver (§9.1),
// and a package that reimplemented any of it would be reimplementing six named defects of
// the legacy application along with it.
//
// # Why it is not registered, and must not be
//
// cmd/openscale/drivers.go registers the protocols a station can name in scale.type. An
// entry there is a line in a drop-down list a volunteer picks from, so registering a toy
// would offer them a protocol no scale on the parc speaks — the reasoning drivers.go already
// applies to `sbpl` on the printing side. cmd/openscale/drivers_test.go asserts the absence.
package example

import (
	"time"

	"openscale/internal/domain"
	"openscale/internal/scale"
	"openscale/internal/scale/serial"
	"openscale/internal/station/ports"
)

// ID is the registry key and would be the value of scale.type.
//
// It names a HARDWARE PROTOCOL and nothing else (§9.3). `manual` is a STATE a station
// enters and `replay` is a DIAGNOSTIC TOOL; the registry panics on both by name, because
// the previous design put a protocol, a degraded mode and a test tool in one drop-down list
// and made « why is this station in manual entry? » undecidable on the morning of a
// breakdown.
//
// Lower case, digits and hyphens: the lookup is an exact string comparison, and the case of
// a suffix is precisely what split the legacy code into two functions for one protocol.
//
// TODO(driver): name the protocol, not the shop and not the station.
const ID = "example-scale"

// Label is what a volunteer reads in the menu of the administration screen.
//
// It is the name PRINTED ON THE HARDWARE — « GRAM XFOC + », with the space before the plus
// — because somebody replacing a scale looks in the menu for what is on the sticker, not
// for a configuration key.
//
// TODO(driver): copy the sticker.
const Label = "Balance d'exemple (protocole jouet)"

// nominalRate is the cadence this protocol DECLARES, used only until the rate meter holds
// eight intervals of its own (§6.5).
//
// Here it is a DEFINITION, because the protocol is defined in this package. Yours is a
// MEASUREMENT, and the distinction is the whole of §21 n° 3: the 400 ms that circulated for
// years about the GRAM was the polling period of an Access timer, and the real scale turned
// out to emit every 96 to 103 ms. That is why the expiry of a measurement is DERIVED from
// the observed cadence instead of from this constant.
//
// TODO(driver): `openscale capture --port COM8 --duration 30m` AT PEAK HOUR, then read the
// median off the summary. Do not copy a figure out of a manual.
const nominalRate = 100 * time.Millisecond

// capabilities is what this protocol honestly declares.
//
// HONESTLY, and that word carries a decision: the empty weight source of
// internal/scale/absent declares an EMPTY set rather than pretending to be a scale, so the
// engine needs no special case for it. A capability declared and not delivered is a
// safeguard the station believes it has.
//
// TODO(driver): Stability only if the frame really carries a stable/moving flag — otherwise
// the latch falls back on its variation criterion, which is a supported answer. Overload
// only if the frame carries an over-capacity flag: no arithmetic on a saturated reading can
// replace it, because a scale over capacity may report any mass at all, including a
// plausible one (§6.4, safeguard rule 1). Tare only if the model takes a tare command ON THE
// WIRE — no scale of this parc does, the tare is typed on screen, and the retare sequence of
// the legacy application was never once emitted in six years (§19).
var capabilities = domain.Capabilities{Stability: true, Overload: false, Tare: false}

// Descriptor reports the identity of this protocol and what it declares.
//
// A VALUE and not a state: two calls answer the same thing forever, which is what lets the
// registry, the administration form and the journal each read it separately.
func Descriptor() domain.ScaleDescriptor {
	return domain.ScaleDescriptor{
		ID:           ID,
		Label:        Label,
		NominalRate:  nominalRate,
		Capabilities: capabilities,
	}
}

// NewDecoder builds ONE decoder of this protocol.
//
// A FUNCTION and not a value, and this is the mistake with the worst consequence in the
// whole package: a decoder holds the bytes waiting for the rest of their frame, so two
// ports — or two entries of one registry — sharing a buffer would complete half a frame of
// the first with the bytes of the second. That is a mass nobody weighed, on a label
// somebody sticks on a bag.
//
// It is exported because the corpus harness of this package hands it over, and because
// `openscale capture`, `openscale replay`, the detection of §14.4 and the « Rejouer cette
// trame » button each ask the registry for one.
func NewDecoder() domain.Decoder { return &Decoder{} }

// Driver is the registry entry of this protocol, and the ONE value a composition root would
// take from this package. It is the « ONE LINE » of §5.2.
func Driver() scale.Driver {
	descriptor := Descriptor()
	return scale.Driver{
		Descriptor: descriptor,
		// The seven options of a serial link, declared ONCE in the shared package: a model
		// that re-declared them would be a form the administration screen generates from
		// one list and a parser that reads another.
		//
		// TODO(driver): a protocol reached any other way declares its OWN schema here —
		// an address, a vendor library path — and the keys it declares are exactly the keys
		// it reads.
		Options: serial.OptionSchema(),
		// MANDATORY. A driver with no decoder factory could be configured and never
		// captured, never detected and never replayed, and the registry panics rather than
		// let the omission surface on the morning somebody needs a capture.
		NewDecoder: NewDecoder,
		// This protocol talks on its own, continuously, so listening on a port for three
		// seconds is enough to recognise it.
		//
		// TODO(driver): scale.EndpointNone for a model that only answers a poll, or one
		// reached through a vendor library. It is the ZERO VALUE on purpose and it is a
		// LEGITIMATE declaration: the screen then names the protocol and tells the
		// volunteer to choose it by hand, which is a sentence they can act on — where a
		// detection button whose only possible answer is silence sends them looking for a
		// cable (§14.4).
		Endpoint: scale.EndpointSerialPort,
		New: func(o domain.DriverOptions, clk ports.Clock, log ports.TechnicalLog) (ports.Scale, error) {
			return newScale(descriptor, o, clk, log, nil)
		},
	}
}

// newScale builds one driver instance around the shared reader loop.
//
// open is the SEAM of §9.1 and it is nil in production, which serial reads as
// OpenSystemPort: a serial port cannot be opened by `go test`, so the tests hand back a
// stream of their own instead. It is what makes the reconnection, the backoff progression,
// the frame cut between two reads and the blocking close testable at all.
//
// The decoder is built HERE, once per instance, and that is not an accident: see NewDecoder.
func newScale(d domain.ScaleDescriptor, o domain.DriverOptions, clk ports.Clock,
	log ports.TechnicalLog, open serial.Opener) (ports.Scale, error) {
	link, err := serial.ParseOptions(o)
	if err != nil {
		return nil, err
	}
	link.Decoder = NewDecoder()
	link.Clock = clk
	link.Open = open
	return serial.New(d, link, log), nil
}
