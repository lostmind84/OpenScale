package main

import (
	"context"
	"net"

	"openscale/internal/domain"
	"openscale/internal/platform"
	"openscale/internal/scale"
	"openscale/internal/scale/serial"
	"openscale/internal/station"
	"openscale/internal/station/ports"
	"openscale/internal/web"
)

// This file answers the « what is actually plugged in? » questions of the expert screens
// (§14.4). Every one of them is platform-specific, which is why none of them lives in
// internal/web.
//
// What is left here is the ENUMERATION — which ports and which print destinations this
// machine has. Listening to one of those ports is in detect.go, the aperçu of the
// label in preview.go, and the frame replayed from the journal in framereplay.go.

// adminHardware is everything the administration screens ask of the machine itself.
type adminHardware struct {
	clock ports.Clock
	// hub is read for the label in flight and written for a replayed frame. It is the
	// station's own loop: nothing here decides anything about a weighing.
	hub *station.Hub
	// registries is what this binary actually carries: the label catalogue the preview
	// reads, and the driver descriptors a refusal names.
	registries domain.Registries
	// scales is the SCALE registry itself and not only its descriptors, because a
	// detection needs what a descriptor cannot carry: the decoder of each protocol, built
	// fresh, and the declaration of whether that protocol can be recognised at all.
	//
	// A nil registry is a binary with no weighing protocol in it, and the detection says
	// so rather than listening to a port for three seconds with nothing to recognise it
	// with.
	scales *scale.Registry
	// technical is where an administrative action is recorded — a replayed frame is one.
	technical ports.TechnicalLog
	// config reports the configuration IN FORCE, because a detection listens with the
	// link settings THIS station declares and not only with those of the parc — a scale
	// that is not at 9600 bauds would otherwise stay undetectable.
	//
	// It is a function and not h.hub.Config() on purpose: Hub.Config dereferences the
	// station's own configuration, so a test building this struct without a hub would
	// panic. nil therefore means « nothing declared yet », which is exactly the case of
	// a station being installed — the very moment this route is used.
	config func() domain.Config

	// open opens a serial port. nil means the real one, so the production path needs no
	// wiring — and a test drives the detection through a stream it hands back, exactly as
	// `openscale capture` does (§9.1).
	open serial.Opener
	// dial and subnets are the two seams of the network scan, for the same reason.
	dial    func(ctx context.Context, address string) (net.Conn, error)
	subnets func() ([]net.IP, error)
}

// Ports enumerates the serial ports of this machine, with the USB description that makes
// one recognisable (§14.4).
func (h adminHardware) Ports(ctx context.Context) ([]web.PortInfo, error) {
	found, err := platform.SerialPorts(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]web.PortInfo, 0, len(found))
	for _, port := range found {
		out = append(out, web.PortInfo{
			Name: port.Name, Description: port.Description, VID: port.VID, PID: port.PID,
		})
	}
	return out, nil
}

// Printers enumerates the print destinations the platform knows about.
func (h adminHardware) Printers(ctx context.Context) ([]web.PrinterInfo, error) {
	queues, err := platform.PrintQueues(ctx)
	if err != nil {
		return nil, err
	}
	return printersOf(queues), nil
}

// DiscoverPrinters scans the local /24 for something listening on the raw print port.
//
// What it finds is a CANDIDATE and not a printer, and the wording says so: a host that
// accepts a connection on 9100 may be a proxy or a switch. The operator picks from the
// list; nothing here writes an address into a configuration.
func (h adminHardware) DiscoverPrinters(ctx context.Context) ([]web.PrinterInfo, error) {
	found, err := platform.DiscoverPrinters(ctx, platform.DiscoverOptions{
		Clock: h.clock, Budget: platform.DiscoverBudget,
		Dial: h.dial, Subnets: h.subnets,
	})
	if err != nil {
		return nil, err
	}
	return printersOf(found), nil
}

// printersOf converts what the platform enumerated.
func printersOf(queues []platform.PrintQueue) []web.PrinterInfo {
	out := make([]web.PrinterInfo, 0, len(queues))
	for _, queue := range queues {
		out = append(out, web.PrinterInfo{
			Name: queue.Name, Key: queue.Key, Detail: queue.Detail, Default: queue.Default,
		})
	}
	return out
}
