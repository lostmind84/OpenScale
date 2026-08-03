package main

import (
	"context"
	"fmt"
	"strings"

	"openscale/internal/domain"
	"openscale/internal/printing"
	"openscale/internal/scale"
	"openscale/internal/scale/absent"
	"openscale/internal/station/ports"
)

// This file builds, out of one configuration, the three things a station weighs and
// prints with: the label templates, the weight source and the print service. None of
// them is ever a reason to refuse to start (guiding principle 7) — a scale that cannot
// be opened falls back on manual entry, and a printer that cannot be built says so, in
// French, on every button of the troubleshooting screen.

// templatesFor resolves the label layouts this station runs on, with the operator's
// offset RECOMPOSED into the geometry.
//
// THE OFFSET IS CARRIED BY THE TEMPLATE AND BY NOTHING ELSE. printer.options.offset_x
// looks like the <A3> command of the printer language and it is not: the template is
// the only one of the two that the preview screen, the PDF export and the raster driver
// all show, so a volunteer pressing the ±1 dot arrow sees the label move. Feeding both
// would move it twice, and internal/printing/raster refuses such a job outright — see
// the godoc of checkTheOffsetIsAppliedOnce.
func templatesFor(cfg domain.Config, reg domain.Registries) (map[string]domain.Template, error) {
	offsetX, _ := cfg.Printer.Options.Int(optionOffsetX)
	offsetY, _ := cfg.Printer.Options.Int(optionOffsetY)

	shipped := domain.ShippedTemplates()
	out := make(map[string]domain.Template, len(shipped))
	for name, template := range shipped {
		template.OffsetXDots = int(offsetX)
		template.OffsetYDots = int(offsetY)
		out[name] = template
	}
	if _, ok := out[cfg.Printer.Template]; !ok {
		return nil, &serviceFailure{Exit: exitFailure, Message: fmt.Sprintf(
			"printer.template : gabarit inconnu %q ; gabarits disponibles : %s",
			cfg.Printer.Template, strings.Join(reg.TemplateNames(), ", "))}
	}
	return out, nil
}

// buildScale opens the weight source this configuration names, and NEVER refuses to
// start over it.
//
// A scale that cannot be opened is an amber light and a fallback to manual entry, never
// a station that will not start (guiding principle 7): Station.Start already degrades
// on a Start that fails, and this covers the step before — a protocol no driver of this
// binary answers to, or options it cannot read.
func buildScale(cfg domain.Config, reg *scale.Registry, clk ports.Clock, log ports.TechnicalLog) ports.Scale {
	weigher, err := newScale(cfg, reg, clk, log)
	if err != nil {
		log.Technical(domain.LevelError, "scale", "ERR-SCL-03",
			"La balance déclarée n'a pas pu être construite : le poste passe en saisie manuelle.",
			err.Error())
		return absent.New(log)
	}
	return weigher
}

// newScale builds the weight source of one configuration.
//
// A station that declares it has NO scale gets the absent source, and that is an
// explicit declaration rather than an inference: scale.present false turns the light
// off instead of leaving it red, and typing the weight becomes nominal (§9.3).
func newScale(cfg domain.Config, reg *scale.Registry, clk ports.Clock, log ports.TechnicalLog) (ports.Scale, error) {
	if !cfg.Scale.Present {
		return absent.New(log), nil
	}
	return reg.New(cfg.Scale.Type, cfg.Scale.Options, clk, log)
}

// buildPrinter builds the printer this configuration names, and never refuses to start
// over it either.
//
// The station keeps serving with a printer that says, in French, why it cannot print:
// the weighing is still journalled, the reprint bar is still there, and the
// administration screen names the offending key. A station that refused to start over a
// print queue would be a station nobody can reconfigure.
func buildPrinter(cfg domain.Config, reg *printing.Registry, templates map[string]domain.Template,
	clk ports.Clock, log ports.TechnicalLog, dataDir string) ports.Printer {
	printer, err := newPrinter(cfg, reg, templates, clk, log, dataDir)
	if err != nil {
		log.Technical(domain.LevelError, "printer", "ERR-PRN-01",
			"L'imprimante déclarée n'a pas pu être construite.", err.Error())
		return unbuiltPrinter{reason: err.Error()}
	}
	return printer
}

// newPrinter builds the print service of one configuration: the driver
// printer.type names, over the transport printer.options.transport names, with the
// retries and the roll counter of §8.2 around it.
//
// The transport is built ONLY for a driver that declares one. Built first and
// unconditionally, as it was, it refused the empty `printer.options` of the neutral profile
// before the driver was ever consulted — so a station in factory configuration got
// `unbuiltPrinter` and answered « l'imprimante configurée n'a pas pu être construite » to
// every button of the troubleshooting screen, which is the one screen that station exists
// to serve.
func newPrinter(cfg domain.Config, reg *printing.Registry, templates map[string]domain.Template,
	clk ports.Clock, log ports.TechnicalLog, dataDir string) (ports.Printer, error) {
	var byteLayer ports.Transport
	if declaresTransport(reg, cfg.Printer.Type) {
		opened, err := newTransport(cfg.Printer.Options, clk, labelsDir(dataDir))
		if err != nil {
			return nil, err
		}
		byteLayer = opened
	}
	driver, err := reg.New(cfg.Printer.Type, printing.DriverConfig{
		Options:   cfg.Printer.Options,
		Transport: byteLayer,
		OutputDir: previewsDir(dataDir),
		Template:  templates[cfg.Printer.Template],
		Clock:     clk,
		Log:       log,
		DemoLabel: func() (domain.Label, error) { return demoLabel(cfg.Pricing) },
	})
	if err != nil {
		if byteLayer != nil {
			_ = byteLayer.Close()
		}
		return nil, err
	}
	capacity, _ := cfg.Printer.Options.Int(optionRollCapacity)
	service, err := printing.NewService(printing.ServiceOptions{
		Main: driver,
		// A driver with no transport names itself: printing.NewService falls back on the
		// label of the descriptor, which for `preview` already says it prints nothing.
		MainName: describeTransport(byteLayer),
		Clock:    clk,
		// The roll counter has NO persistent store yet: internal/store carries no
		// AddLabels/SetLabels pair, so the count restarts with the process. What it
		// still buys is the 90 % amber light within one service, and « J'ai changé le
		// rouleau » still resets it.
		Roll: printing.NewRollCounter(nil, int(capacity), log),
		Log:  log,
	})
	if err != nil {
		// Returned as a NIL INTERFACE and never as a typed nil pointer: a caller
		// checking `printer != nil` on a *Service that failed to build would get true.
		_ = driver.Close()
		return nil, err
	}
	return service, nil
}

// declaresTransport reports whether the driver printer.type names carries a byte transport
// at all.
//
// The answer comes from the driver's OWN option schema — the same schema control 7 of
// §11.3 validates printer.options against and the administration screen generates its form
// from — so a driver added later says for itself whether the composition root has a device
// to open for it, with no list to keep in step here.
//
// An unknown driver answers false, and that is deliberate: reg.New refuses it one line
// later with the list of the ones that exist, and building a transport first would replace
// that refusal with a complaint about a transport key nobody typed.
func declaresTransport(reg *printing.Registry, driverID string) bool {
	for _, descriptor := range reg.Descriptors() {
		if descriptor.ID != driverID {
			continue
		}
		for _, option := range descriptor.Options {
			if option.Key == optionTransport {
				return true
			}
		}
	}
	return false
}

// describeTransport is the French name of the byte layer, or nothing when there is none.
func describeTransport(byteLayer ports.Transport) string {
	if byteLayer == nil {
		return ""
	}
	return byteLayer.Describe()
}

// unbuiltPrinter is what a station has when its configuration names a printer this
// binary cannot build.
//
// It exists so that the station STARTS anyway: station.New requires a printer, and a
// nil one would take the whole poste out of service over a queue name. Every refusal it
// answers is KindConfig — no retry, and the administration screen shows what is
// configured against what actually exists (§8.5).
type unbuiltPrinter struct{ reason string }

// Descriptor reports a driver that exists in name only.
func (p unbuiltPrinter) Descriptor() domain.PrinterDescriptor {
	return domain.PrinterDescriptor{ID: "unbuilt", Label: "Imprimante non construite"}
}

// Print refuses, naming why the printer could not be built.
func (p unbuiltPrinter) Print(context.Context, ports.PrintJob) (ports.PrintReceipt, error) {
	return ports.PrintReceipt{}, p.refuse("printer.Print")
}

// Status reports that nothing can be known about a printer that was never opened.
func (p unbuiltPrinter) Status(context.Context) ports.PrinterStatus {
	return ports.PrinterStatus{Health: ports.PrinterFaulted,
		Detail: "l'imprimante configurée n'a pas pu être construite : " + p.reason}
}

// SelfTest refuses, for the same reason Print does.
func (p unbuiltPrinter) SelfTest(context.Context, string) error { return p.refuse("printer.SelfTest") }

// Close has nothing to release.
func (p unbuiltPrinter) Close() error { return nil }

func (p unbuiltPrinter) refuse(op string) error {
	return &ports.PrintError{Kind: ports.KindConfig, Op: op, Message: "l'imprimante configurée " +
		"n'a pas pu être construite : " + p.reason}
}
