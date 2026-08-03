package raster

// This file is level N3 of §8.5 as this driver answers it: one ENQ over the transport,
// a budget spent on the injected clock, and a verdict that never turns a silence into a
// failure — nor an answer into a « prête » the device never said.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"openscale/internal/printing/sbpl"
	"openscale/internal/station/ports"
)

// statusBudget is how long a status probe waits for the printer to say something
// (§8.5, level N3). It bounds a transport that answers, never a weighing.
//
// It stays HERE, next to the driver that spends it, where the ENQ byte and the reading
// of the answer have gone to internal/printing/sbpl: how long a station is willing to
// wait is a policy of the station, and the SATO reference states no such delay.
const statusBudget = 500 * time.Millisecond

// Status reports what the device says about itself, or an honest admission that we do
// not know (§8.5).
//
// It NEVER turns a silence into a failure. A transport that cannot ask answers
// PrinterUnknown, which is the whole reason that value exists, and a printer that
// stays quiet for 500 ms is reported as unknown rather than faulted: confirming a
// physical event with a probe that does not observe it is exactly the mistake
// important-7 removed.
func (p *Printer) Status(ctx context.Context) ports.PrinterStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ports.PrinterStatus{Health: ports.PrinterUnknown,
			Detail: "l'imprimante a été fermée par le poste."}
	}

	answer, err := p.transport.Query(ctx, sbpl.Enquiry(), statusBudget)
	switch {
	case errors.Is(err, ports.ErrUnsupported):
		return ports.PrinterStatus{Health: ports.PrinterUnknown,
			Detail: fmt.Sprintf("état inconnu : %s ne peut pas interroger l'imprimante. "+
				"L'étiquette part, la réponse ne revient pas.", p.transport.Describe())}
	case err != nil:
		return ports.PrinterStatus{Health: ports.PrinterFaulted, Raw: answer,
			Detail: fmt.Sprintf("l'imprimante n'a pas répondu (%s) : %v", p.transport.Describe(), err)}
	case len(answer) == 0:
		return ports.PrinterStatus{Health: ports.PrinterUnknown,
			Detail: fmt.Sprintf("état inconnu : %s n'a rien renvoyé en %s.",
				p.transport.Describe(), statusBudget)}
	}
	// The frame IS decoded now — the L0 bench captured it — but only far enough to name
	// a FAULT. Read sbpl.FaultOfStatusFrame for why readiness is still never claimed.
	if fault, named := sbpl.FaultOfStatusFrame(answer); named {
		return ports.PrinterStatus{Health: fault.Health, Raw: answer,
			Detail: fmt.Sprintf("%s (%s).", fault.Reason, p.transport.Describe())}
	}

	// Any non-empty answer means the printer is ALIVE (§8.5) — and alive is not ready.
	// PrinterReady means « answered and has NOTHING TO REPORT » (ports), and this
	// printer does not answer that question when it is idle: see sbpl.FaultOfStatusFrame.
	// Claiming ready here would be a green light on /readyz over an empty roll (§14.5).
	//
	// The detail names the TRANSPORT and stops there. What the answer means is the
	// aggregation's sentence (internal/printing/status.go), and a driver that spelled
	// the same conclusion produced it twice in a row on the troubleshooting screen.
	return ports.PrinterStatus{Health: ports.PrinterUnknown, Raw: answer,
		Detail: p.transport.Describe()}
}
