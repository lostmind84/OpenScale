package main

import (
	"fmt"
	"sort"
	"strings"

	"openscale/internal/catalog"
	"openscale/internal/catalog/localdrop"
	"openscale/internal/catalog/webdav"

	"openscale/internal/domain"
	"openscale/internal/printing"
	"openscale/internal/printing/preview"
	"openscale/internal/printing/raster"
	"openscale/internal/printing/transport"
	"openscale/internal/scale"
	"openscale/internal/scale/gramxfoc"
	"openscale/internal/station/ports"
)

// THIS FILE IS THE ONLY ONE THAT NAMES A CONCRETE DRIVER (cut 2 of §5.2).
//
// internal/station sees ports.Scale and ports.Printer, internal/web sees a Hub, and
// neither can reach a serial port or a print queue. Adding a scale model is ONE
// PACKAGE and ONE LINE below, with zero modification anywhere else: the
// administration screen discovers the driver through the registry and generates its
// form from the option schema the driver itself declares (§9.3, §11.3).
//
// It names them and NOTHING ELSE. A driver is a value its own package hands over,
// complete — identity, capabilities, option schema and factory — and the two registries
// below are lists of such values. What a driver needs from a configuration is the
// driver's business: a composition root that spelled the option keys of a printer would
// have to be edited for every option that printer ever gains, and a bound written here
// would go stale in silence the day the hardware changes.

// scaleRegistry is the set of weighing PROTOCOLS this binary was built with.
//
// Two entries and one decoder: the GRAM XFOC RS and the GRAM XFOC + differ by the
// name on the sticker and by nothing on the wire (§9.3). `manual` and `replay` are
// deliberately absent, and the registry refuses them by name — the first is a STATE
// the station enters, the second a DIAGNOSTIC TOOL (openscale capture / replay).
func scaleRegistry() *scale.Registry {
	registry := scale.NewRegistry()
	for _, driver := range gramxfoc.Drivers() {
		registry.Register(driver)
	}
	return registry
}

// printerRegistry is the set of printer drivers this binary was built with.
//
// TWO entries. `raster` is the production path (ADR-002): at 203 dpi no whole module
// reproduces the magnification of the label the cooperative prints, so the bitmap is drawn
// dot by dot. `preview` writes that same bitmap to a PNG and a life-size PDF and opens no
// device at all — and it is not a convenience, it is what the NEUTRAL PROFILE names
// (§11.3). A binary that did not carry it served, on every station whose configuration is
// unusable, a profile its own validation refused: `printer.type` came back as a fault on a
// field nobody had touched, and no save was possible from that screen at all.
//
// `sbpl` is named by §8.1 and by domain.PrinterTypes and is still not registered:
// internal/printing/sbpl is the shared ENCODER of the frame, not a ports.Printer.
// Registering a driver that does not exist would put a value in a volunteer's drop-down
// list that no station can honour.
func printerRegistry() *printing.Registry {
	registry := printing.NewRegistry()
	registry.Register(raster.Driver())
	registry.Register(preview.Driver())
	return registry
}

// The keys of printer.options THIS ROOT READS FOR ITSELF, spelled as config.json carries
// them (§11.2). The driver's own schema is the authority on the spelling; what is listed
// here is the short list of keys whose value never reaches the driver as an option.
//
//	transport, queue, path, address  build the byte layer (§8.4) — a driver that opened a
//	                                 device itself would lose « one frame, four
//	                                 destinations »
//	offset_x, offset_y               shift the TEMPLATE, which is the only one of the two
//	                                 offsets the preview screen shows (raster.ParseOptions)
//	roll_capacity                    sizes the roll counter, which counts labels for a
//	                                 station and not for a printer
//
// Everything else in printer.options is read by raster.ParseOptions, in the package that
// declares it.
const (
	optionTransport    = "transport"
	optionQueue        = "queue"
	optionPath         = "path"
	optionAddress      = "address"
	optionOffsetX      = "offset_x"
	optionOffsetY      = "offset_y"
	optionRollCapacity = "roll_capacity"
)

// newTransport reads the keys above and lets the byte layer build itself (§8.4).
//
// WHICH transports exist, and how each one is built, belong to the package that owns the
// list — this root would otherwise hold a fourth copy of it, next to the administration
// screen, the option schema of the driver and control 8 of Config.Validate. What stays
// here is the one thing only a composition root can do: it BUILDS the transport and it
// CLOSES it, and no printer driver ever opens a device (transport.New says why that
// clause is load-bearing).
//
// labelDir is <data>/labels, where the `file` transport drops one copy per label — a
// directory the service owns, so that « envoyez-moi le fichier de la dernière
// étiquette » is a sentence a volunteer can act on.
func newTransport(o domain.DriverOptions, clk ports.Clock, labelDir string) (ports.Transport, error) {
	name, _ := o.Text(optionTransport)
	queue, _ := o.Text(optionQueue)
	path, _ := o.Text(optionPath)
	address, _ := o.Text(optionAddress)
	return transport.New(name, transport.Config{
		Queue:    queue,
		Path:     path,
		Address:  address,
		Clock:    clk,
		LabelDir: labelDir,
	})
}

// catalogSources is the set of catalog sources this binary was built with.
//
// Two entries, and the split between them is not technical: `local_drop` watches a
// directory this machine can see, `webdav` fetches from a share. Both end at the same
// place — ONE CSV per station, deleted once read, and the deletion IS the
// acknowledgement (§10.1). A single shared file that four stations would have to agree
// on has no acknowledgement at all, which is why it is not offered.
func catalogSources() map[string]catalog.Source {
	sources := make(map[string]catalog.Source, 2)
	for _, source := range []catalog.Source{localdrop.Descriptor(), webdav.Descriptor()} {
		sources[source.ID] = source
	}
	return sources
}

// newCatalogSource builds the source a configuration names.
//
// It refuses an unknown type by NAMING what exists, like every other registry here: a
// volunteer who mistyped a driver name must read the list rather than the word
// "inconnu" (§11.3).
func newCatalogSource(
	cfg domain.Config, dataDir string, clock ports.Clock, log ports.TechnicalLog,
	images catalog.ImageSink, quarantine catalog.FailureLedger,
) (ports.CatalogSource, error) {
	sources := catalogSources()
	source, known := sources[cfg.Catalog.Type]
	if !known {
		names := make([]string, 0, len(sources))
		for id := range sources {
			names = append(names, id)
		}
		sort.Strings(names)
		return nil, fmt.Errorf("catalog.type %q inconnu : les sources disponibles sont %s",
			cfg.Catalog.Type, strings.Join(names, ", "))
	}
	return source.New(catalog.SourceConfig{
		Catalog:       cfg.Catalog,
		StationNumber: cfg.Station.Number,
		DataDir:       dataDir,
		Clock:         clock,
		Log:           log,
		Images:        images,
		Quarantine:    quarantine,
	})
}

// registries is what Config.Validate checks a configuration against.
//
// It is extracted from serve so that a TEST can validate a shipped configuration the way
// the station does, registries included. Validating against an empty set would accept a
// driver name no binary carries — which is exactly the mistake that leaves an operator
// with an amber light and an empty grid instead of a fault next to the field.
func registries() domain.Registries {
	scales, printers := scaleRegistry(), printerRegistry()
	return domain.Registries{
		Scales:         scales.Descriptors(),
		Printers:       printers.Descriptors(),
		Transports:     transport.Descriptors(),
		CatalogSources: catalogSourceDescriptors(),
	}
}
