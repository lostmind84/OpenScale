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
	registry.Register(rasterDriver())
	registry.Register(previewDriver())
	return registry
}

// previewDriver is the registry entry of the driver that prints nothing.
//
// IT DECLARES NO OPTION, and that is the requirement rather than an omission. The neutral
// profile carries no `printer.options` block — darkness, speed and the number of copies are
// settled on a real print run, and a factory profile has no business inventing three
// figures nobody measured — so the driver such a station falls back on must ask for none of
// them. An empty schema is also what tells the composition root there is no transport to
// build here.
func previewDriver() printing.Driver {
	return printing.Driver{
		Descriptor: domain.PrinterDescriptor{
			ID:    preview.ID,
			Label: preview.Label,
			Capabilities: domain.PrinterCapabilities{
				Raster:    true,
				Status:    false,
				Cutter:    false,
				MaxCopies: 1,
			},
		},
		New: func(c printing.DriverConfig) (ports.Printer, error) {
			return preview.New(preview.Options{
				Dir:       c.OutputDir,
				Clock:     c.Clock,
				Log:       c.Log,
				Template:  c.Template,
				DemoLabel: c.DemoLabel,
			})
		},
	}
}

// rasterDriver is the registry entry of the production printer driver.
//
// The head is a WS408 — 8 dots/mm, 104 bytes of <G> block — because that is the whole
// parc, and raster.WS408 is where the two figures come from rather than from here.
func rasterDriver() printing.Driver {
	head := raster.WS408()
	return printing.Driver{
		Descriptor: domain.PrinterDescriptor{
			ID:    raster.ID,
			Label: raster.Label,
			Capabilities: domain.PrinterCapabilities{
				Raster:    true,
				Status:    true,
				Cutter:    false,
				MaxCopies: raster.MaxCopies,
				DotsPerMM: head.DotsPerMM,
			},
		},
		Options: printerOptionSchema(),
		New: func(c printing.DriverConfig) (ports.Printer, error) {
			settings, err := rasterSettings(c.Options)
			if err != nil {
				return nil, err
			}
			return raster.New(raster.Options{
				Transport: c.Transport,
				Clock:     c.Clock,
				Log:       c.Log,
				Template:  c.Template,
				Settings:  settings,
				Head:      head,
				DemoLabel: c.DemoLabel,
			})
		},
	}
}

// The keys of printer.options, spelled exactly as config.json carries them (§11.2).
const (
	optionTransport    = "transport"
	optionQueue        = "queue"
	optionPath         = "path"
	optionAddress      = "address"
	optionFallback     = "fallback"
	optionEnabled      = "enabled"
	optionDarkness     = "darkness"
	optionSpeed        = "speed"
	optionOffsetX      = "offset_x"
	optionOffsetY      = "offset_y"
	optionInvertBits   = "invert_bits"
	optionCopies       = "copies"
	optionRollCapacity = "roll_capacity"
)

// printerOptionSchema declares printer.options, which is what lets the administration
// screen GENERATE its form and control 7 of Config.Validate check the OPTIONS of the
// driver instead of only its type name (§11.3).
//
// darkness, speed and copies are REQUIRED, and that is the rule raster.Settings states
// in its own words: the zero value is not a configuration, a darkness of zero is not a
// shade of grey. Declaring them required is what makes a file that forgot one produce a
// fault in the ALL-AT-ONCE list of §11.3, next to the field that carries it, instead of
// a refusal with a customer standing at the scale.
//
// queue, path and address are NOT required: each belongs to one transport and is empty
// for the other three. Which one is needed is the transport's business, and each says so
// in French when it is built.
func printerOptionSchema() []domain.OptionSchema {
	fallback := []domain.OptionSchema{
		{Key: optionEnabled, Kind: domain.OptionBool},
		{Key: optionTransport, Kind: domain.OptionEnum, Values: transportNames()},
		{Key: optionQueue, Kind: domain.OptionText},
		{Key: optionPath, Kind: domain.OptionText},
		{Key: optionAddress, Kind: domain.OptionHostPort},
	}
	return []domain.OptionSchema{
		{Key: optionTransport, Kind: domain.OptionEnum, Required: true, Values: transportNames()},
		{Key: optionQueue, Kind: domain.OptionText},
		{Key: optionPath, Kind: domain.OptionText},
		{Key: optionAddress, Kind: domain.OptionHostPort},
		{Key: optionFallback, Kind: domain.OptionGroup, Options: fallback},
		{Key: optionDarkness, Kind: domain.OptionInt, Required: true,
			Min: raster.MinDarkness, Max: raster.MaxDarkness},
		{Key: optionSpeed, Kind: domain.OptionInt, Required: true,
			Min: raster.MinSpeed, Max: raster.MaxSpeed},
		// The offsets are bounded HERE by the four digits of the <A3> field and by
		// control 38 against the geometry of the template, which is the bound that
		// really matters — beyond it the inked content leaves the label.
		{Key: optionOffsetX, Kind: domain.OptionInt, Min: -raster.MaxOffsetDots, Max: raster.MaxOffsetDots},
		{Key: optionOffsetY, Kind: domain.OptionInt, Min: -raster.MaxOffsetDots, Max: raster.MaxOffsetDots},
		{Key: optionInvertBits, Kind: domain.OptionBool},
		{Key: optionCopies, Kind: domain.OptionInt, Required: true, Min: 1, Max: 10},
		{Key: optionRollCapacity, Kind: domain.OptionInt, Min: 50, Max: 100_000},
	}
}

// transportNames reports the byte transports a volunteer may choose from, in the order
// the transport package declares them.
func transportNames() []string {
	descriptors := transport.Descriptors()
	names := make([]string, 0, len(descriptors))
	for _, d := range descriptors {
		names = append(names, d.ID)
	}
	return names
}

// rasterSettings reads the three adjustments of §8.2 out of printer.options.
//
// IT DELIBERATELY LEAVES THE OFFSET AT ZERO, and that is the whole point of the guard
// raster.checkTheOffsetIsAppliedOnce documents. printer.options.offset_x feeds the
// TEMPLATE, because the template is the only one of the two the preview screen shows:
// a volunteer pressing the ±1 dot arrow must see the label move on the screen they are
// adjusting against. Wired naively, one key would feed both the layout and the <A3>
// command and the label would move twice — and nobody would find out until a roll had
// been spoiled.
func rasterSettings(o domain.DriverOptions) (raster.Settings, error) {
	settings := raster.Settings{}
	for _, field := range []struct {
		key  string
		into *int
	}{
		{optionDarkness, &settings.Darkness},
		{optionSpeed, &settings.Speed},
		{optionCopies, &settings.Copies},
	} {
		value, ok := o.Int(field.key)
		if !ok {
			return raster.Settings{}, fmt.Errorf(
				"printer.options.%s : cette valeur se règle sur un tirage réel et le fichier doit la porter",
				field.key)
		}
		*field.into = int(value)
	}
	settings.InvertBits, _ = o.Bool(optionInvertBits)
	return settings, nil
}

// newTransport builds the byte layer printer.options.transport names (§8.4).
//
// It lives in the composition root and not in the printer driver, and that is what lets
// ONE frame reach four destinations: `raster` carries a frame, it never opens a device.
// labelDir is <data>/labels, where the `file` transport drops one copy per label — a
// directory the service owns, so that « envoyez-moi le fichier de la dernière
// étiquette » is a sentence a volunteer can act on.
func newTransport(o domain.DriverOptions, clk ports.Clock, labelDir string) (ports.Transport, error) {
	name, _ := o.Text(optionTransport)
	queue, _ := o.Text(optionQueue)
	path, _ := o.Text(optionPath)
	address, _ := o.Text(optionAddress)
	switch name {
	case domain.TransportWinspool:
		return transport.NewWinspool(transport.WinspoolOptions{Queue: queue})
	case domain.TransportDevfile:
		return transport.NewDevfile(transport.DevfileOptions{Path: path, Clock: clk})
	case domain.TransportTCP:
		return transport.NewTCP(transport.TCPOptions{Address: address, Clock: clk})
	case domain.TransportFile:
		if path == "" {
			path = labelDir
		}
		return transport.NewFile(transport.FileOptions{Dir: path, Clock: clk})
	}
	return nil, fmt.Errorf("printer.options.transport : transport inconnu %q ; transports disponibles : %s",
		name, strings.Join(transportNames(), ", "))
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
