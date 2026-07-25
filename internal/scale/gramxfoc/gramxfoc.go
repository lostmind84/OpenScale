// Package gramxfoc drives the two GRAM XFOC scales of the parc.
//
// TWO REGISTRY ENTRIES, ONE DECODER (§9.3). The legacy application carried two
// functions, ReformatePoidsBalanceXFOCRS and ReformatePoidsBalanceXFOCPLUS, differing
// by the case of a suffix, an extraction window of 8 versus 7 characters and their
// behaviour on a short frame. Those are not protocol differences: they are two
// diverging copies of the same fixed-window code, and the case-insensitive grammar of
// internal/domain/frame covers both. What justifies two entries is therefore not the
// wire but the sticker on the hardware — a volunteer replacing a scale looks in the
// menu for the name printed on the device, « GRAM XFOC RS » or « GRAM XFOC + ».
//
// So this package holds wiring and nothing else: the shared loop of
// internal/scale/serial, the accumulator of the pure core, two descriptors. That is
// the measure §9.3 sets for adding a model — one package, three files, ~120 lines of
// which 70 are tests — and anything more here would be something already written
// somewhere else.
package gramxfoc

import (
	"time"

	"openscale/internal/domain"
	"openscale/internal/domain/frame"
	"openscale/internal/scale"
	"openscale/internal/scale/serial"
	"openscale/internal/station/ports"
)

// The two values of scale.type this package answers to. They are the registry keys, so
// they are lower case and hyphenated: the lookup is an exact string comparison, and the
// case of a suffix is exactly what split the legacy code in two.
const (
	// IDRS is the GRAM XFOC RS.
	IDRS = "gram-xfoc-rs"
	// IDPlus is the GRAM XFOC +.
	IDPlus = "gram-xfoc-plus"
)

// The labels, spelled as they are printed on the hardware. French is irrelevant here —
// they are brand names — but the shape is not: « GRAM XFOC + » with its space before
// the plus is what is on the sticker.
const (
	labelRS   = "GRAM XFOC RS"
	labelPlus = "GRAM XFOC +"
)

// nominalRate is the cadence both models DECLARE, used only until the rate meter holds
// eight intervals of its own (§6.5).
//
// 400 ms, and the figure carries its own warning: it is the Form_Timer polling period
// of the legacy Access form, NOT a measured emission rate (§21 n° 3). That is precisely
// why the expiry of a measurement is DERIVED from the observed cadence rather than from
// this constant, and why the 30-minute peak-hour capture of lot L0 is what will turn it
// into a measurement.
const nominalRate = 400 * time.Millisecond

// capabilities is what both models honestly declare.
//
// Stability and Overload are true: every frame of the living corpus carries the ST/US
// flag, and the grammar reads OL — the corpus holds an "OL,GS,+ 99.999KG" whose mass is
// meaningless and whose flag has to reach safeguard rule 1 (§6.4). Tare stays FALSE: no
// scale of this parc takes a tare command on the wire, the tare is typed on screen, and
// the retare sequence of the legacy application was never once emitted in six years
// (§19).
var capabilities = domain.Capabilities{Stability: true, Overload: true}

// Drivers returns the two registry entries of the GRAM family, in the order a volunteer
// reads them.
//
// It is what cmd/openscale/drivers.go registers, and it is deliberately a LIST rather
// than two exported values: the two entries share one implementation, and a caller that
// could register one without the other would be describing a binary that does not exist.
func Drivers() []scale.Driver {
	return []scale.Driver{
		driverFor(Descriptor(IDRS)),
		driverFor(Descriptor(IDPlus)),
	}
}

// Descriptor reports the identity of one GRAM model. An unknown id yields the zero
// descriptor, which no registry accepts.
//
// It is exported because the composition root and the journal both name a model without
// building one — and because a descriptor is a value, not a state: two calls answer the
// same thing forever.
func Descriptor(id string) domain.ScaleDescriptor {
	switch id {
	case IDRS:
		return descriptorOf(IDRS, labelRS)
	case IDPlus:
		return descriptorOf(IDPlus, labelPlus)
	}
	return domain.ScaleDescriptor{}
}

func descriptorOf(id, label string) domain.ScaleDescriptor {
	return domain.ScaleDescriptor{
		ID:           id,
		Label:        label,
		NominalRate:  nominalRate,
		Capabilities: capabilities,
	}
}

// driverFor is one registry entry: an identity, the option schema of a serial link, and
// a factory that builds THE shared loop around a fresh accumulator.
func driverFor(d domain.ScaleDescriptor) scale.Driver {
	return scale.Driver{
		Descriptor: d,
		Options:    serial.OptionSchema(),
		New: func(o domain.DriverOptions, clk ports.Clock, log ports.TechnicalLog) (ports.Scale, error) {
			return newScale(d, o, clk, log, nil)
		},
	}
}

// newScale builds one driver instance.
//
// open is the seam of §9.1 and it is nil in production, which serial reads as
// OpenSystemPort: a serial port cannot be opened by `go test`, so the corpus is replayed
// through a stream a test hands back instead.
//
// The accumulator is built HERE, once per instance, and that is not an accident: two
// stations, or two entries of the same registry, must never share a pending buffer —
// half a frame from one port completed by bytes from the other is exactly the fabricated
// mass the grammar exists to refuse.
func newScale(d domain.ScaleDescriptor, o domain.DriverOptions, clk ports.Clock,
	log ports.TechnicalLog, open serial.Opener) (ports.Scale, error) {
	link, err := serial.ParseOptions(o)
	if err != nil {
		return nil, err
	}
	link.Decoder = &frame.Accumulator{}
	link.Clock = clk
	link.Open = open
	return serial.New(d, link, log), nil
}
